package gateway

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// visionClient answers questions about one video frame with Claude.
type visionClient struct {
	cfg    AIVisionConfig
	client anthropic.Client
}

func newVisionClient(cfg AIVisionConfig) *visionClient {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	return &visionClient{cfg: cfg, client: anthropic.NewClient(opts...)}
}

// visionError carries an HTTP status for the API response.
type visionError struct {
	status int
	msg    string
}

func (e *visionError) Error() string { return e.msg }

const visionSystemPrompt = `You help the operators of a police surveillance drone in Bangladesh understand what its camera shows. You receive one frame from the drone's visible-light or thermal camera, plus the drone's altitude and heading when known.

Answer the operator's question about the frame. If there is no question, describe the scene: the number of people and vehicles you can see (with vehicle types: car, bus, truck, motorbike, bicycle, rickshaw, CNG auto-rickshaw), what they appear to be doing, and anything that needs attention, such as a crowd forming, a fight, an accident, a fire or smoke, a blocked road, or someone in danger.

Rules:
- Report only what is visible. When something is small, blurred or ambiguous, say so instead of guessing, and give counts as ranges when you cannot count exactly.
- Do not try to identify who a person is, and do not guess anyone's religion, ethnicity or political affiliation.
- Thermal frames show heat, not colour: bright usually means warm. Mention it when that limits what you can tell.
- Keep the answer short enough to read during a flight: a few sentences or a short list.`

// frameJPEG fetches the current frame of a stream as JPEG from go2rtc.
func (h *aiHub) frameJPEG(ctx context.Context, stream string) ([]byte, error) {
	base := h.g.cfg.Go2RTC.APIURL
	if base == "" {
		return nil, &visionError{http.StatusServiceUnavailable, "go2rtc is not configured"}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/frame.jpeg?src="+url.QueryEscape(stream), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, &visionError{http.StatusGatewayTimeout, "could not get a frame from go2rtc: " + err.Error()}
	}
	defer resp.Body.Close()
	img, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK || len(img) < 3 || img[0] != 0xFF || img[1] != 0xD8 {
		return nil, &visionError{http.StatusBadGateway, fmt.Sprintf("go2rtc returned no frame for %s (HTTP %d); is the camera online?", stream, resp.StatusCode)}
	}
	return img, nil
}

// analyze grabs the current frame and asks Claude about it.
func (h *aiHub) analyze(ctx context.Context, d *Drone, stream, prompt string) (map[string]any, error) {
	if stream == "" {
		return nil, &visionError{http.StatusUnprocessableEntity, "no camera stream found for this drone"}
	}
	img, err := h.frameJPEG(ctx, stream)
	if err != nil {
		return nil, err
	}
	kind := streamKind(stream)
	question := strings.TrimSpace(prompt)
	if question == "" {
		question = "Describe the scene."
	}
	contextLine := fmt.Sprintf("Camera: %s.", kind)
	st := d.Snapshot()
	if p := st.Position; p != nil {
		contextLine += fmt.Sprintf(" Drone altitude above take-off: %.0f m.", p.AltRelM)
		if p.HeadingDeg != nil {
			contextLine += fmt.Sprintf(" Heading: %.0f°.", *p.HeadingDeg)
		}
	}
	if pitch, _, known := d.gim.estimate(); known {
		contextLine += fmt.Sprintf(" Camera tilt (estimated): %.0f° (0 = horizon, -90 = straight down).", pitch)
	}

	text, model, err := h.vision.ask(ctx, img, contextLine+"\n\n"+question)
	if err != nil {
		return nil, err
	}
	h.log.Info("frame analyzed", "drone", d.cfg.ID, "stream", stream, "model", model)
	return map[string]any{"ok": true, "text": text, "stream": stream, "model": model, "at": time.Now().UTC()}, nil
}

func (v *visionClient) ask(ctx context.Context, jpeg []byte, question string) (text, model string, err error) {
	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(v.cfg.Model),
		MaxTokens: 16000,
		System:    []anthropic.BetaTextBlockParam{{Text: visionSystemPrompt}},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(
				anthropic.NewBetaImageBlock(anthropic.BetaBase64ImageSourceParam{
					Data:      base64.StdEncoding.EncodeToString(jpeg),
					MediaType: anthropic.BetaBase64ImageSourceMediaTypeImageJPEG,
				}),
				anthropic.NewBetaTextBlock(question),
			),
		},
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffort(v.cfg.Effort)},
		// If a safety classifier declines, let the API re-serve the request
		// on a fallback model instead of returning nothing.
		Fallbacks: anthropic.BetaFallbacksParamOfDefault(),
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
	}
	msg, err := v.client.Beta.Messages.New(ctx, params)
	if err != nil {
		var apierr *anthropic.Error
		if errors.As(err, &apierr) {
			switch apierr.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden:
				return "", "", &visionError{http.StatusServiceUnavailable, "the Anthropic API key was rejected (ai.vision.api_key)"}
			case http.StatusTooManyRequests:
				return "", "", &visionError{http.StatusTooManyRequests, "the AI service is rate-limited; try again in a minute"}
			case http.StatusBadRequest, http.StatusNotFound:
				return "", "", &visionError{http.StatusBadGateway, "the AI service rejected the request: " + apierr.Error()}
			default:
				return "", "", &visionError{http.StatusBadGateway, fmt.Sprintf("the AI service failed (HTTP %d)", apierr.StatusCode)}
			}
		}
		return "", "", &visionError{http.StatusBadGateway, "could not reach the AI service: " + err.Error()}
	}
	if msg.StopReason == anthropic.BetaStopReasonRefusal {
		return "", string(msg.Model), &visionError{http.StatusUnprocessableEntity,
			"the AI declined to analyze this frame (" + string(msg.StopDetails.Category) + ")"}
	}
	var parts []string
	for _, block := range msg.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok && strings.TrimSpace(t.Text) != "" {
			parts = append(parts, strings.TrimSpace(t.Text))
		}
	}
	if len(parts) == 0 {
		return "", string(msg.Model), &visionError{http.StatusBadGateway, "the AI returned no text (stop reason: " + string(msg.StopReason) + ")"}
	}
	return strings.Join(parts, "\n\n"), string(msg.Model), nil
}
