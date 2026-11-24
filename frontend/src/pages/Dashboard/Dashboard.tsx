import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from "react";
import { AIPanel } from "../../components/ai/AIPanel";
import { EventModal } from "../../components/ai/EventDetails";
import { Compass } from "../../components/dashboard/Compass";
import { DroneMap } from "../../components/dashboard/DroneMap";
import { GimbalPad } from "../../components/dashboard/GimbalPad";
import { RecentRecordings } from "../../components/dashboard/RecentRecordings";
import { RecordingPreviewModal } from "../../components/dashboard/RecordingPreviewModal";
import { RecordingsLibraryModal } from "../../components/dashboard/RecordingsLibraryModal";
import { StatsPanel } from "../../components/dashboard/StatsPanel";
import { ThermalFilter } from "../../components/dashboard/ThermalFilter";
import { VideoStage, type VideoAI } from "../../components/dashboard/VideoStage";
import { FlightPanel } from "../../components/flight/FlightPanel";
import { SpeakerPanel } from "../../components/speaker/SpeakerPanel";
import { DroneTopBar } from "../../components/layout/DroneTopBar";
import { PageFrame } from "../../components/layout/PageFrame";
import { useConfirm } from "../../components/ui/ConfirmDialog";
import { Panel } from "../../components/ui/Panel";
import { Toast } from "../../components/ui/Toast";
import { useAuth } from "../../context/AuthContext";
import { useDrone } from "../../context/DroneContext";
import { useAIEvents } from "../../hooks/useAIEvents";
import { useAIStatus } from "../../hooks/useAIStatus";
import { useAircraftFeatures } from "../../hooks/useAircraftFeatures";
import { useCameras } from "../../hooks/useCameras";
import { useGimbal } from "../../hooks/useGimbal";
import { useKeyboardShortcuts } from "../../hooks/useKeyboardShortcuts";
import { useRecording } from "../../hooks/useRecording";
import { useSpeaker } from "../../hooks/useSpeaker";
import { useToast } from "../../hooks/useToast";
import { api } from "../../services/api";
import { errorText } from "../../services/http";
import type { AIEvent, Detection } from "../../types/ai";
import type { LinkState } from "../../types/drone";
import type { Recording } from "../../types/recording";
import { labelName, MAP_EVENT_WINDOW_MS } from "../../utils/ai";
import { buildStats } from "../../utils/format";
import { toTelemetry } from "../../utils/telemetry";
import type { AircraftCommand, AircraftFeature, CameraSource } from "../../types/gateway";
import styles from "./Dashboard.module.css";

type SidebarTab = "flight" | "recordings" | "ai" | "speaker" | "thermal" | "aircraft";

const TAB_LABELS: Record<SidebarTab, string> = {
  flight: "Flight",
  recordings: "Recordings",
  ai: "AI",
  speaker: "Speaker",
  thermal: "Thermal",
  aircraft: "Aircraft",
};

// Only drones with an aircraft API (the Mavic 3T) use these: loaded on demand.
const aircraftUi = () => import("../../components/aircraft");
const ThermalPanel = lazy(() => aircraftUi().then((m) => ({ default: m.ThermalPanel })));
const ThermalOverlay = lazy(() => aircraftUi().then((m) => ({ default: m.ThermalOverlay })));
const AircraftPanel = lazy(() => aircraftUi().then((m) => ({ default: m.AircraftPanel })));

const THERMAL_FEATURES: AircraftFeature[] = ["thermal_measure", "thermal_palette", "thermal_gain", "isotherm"];

export function Dashboard() {
  const { message, notify } = useToast();
  const confirm = useConfirm();
  const { canOperate } = useAuth();
  const { id, info, snapshot, sendGimbalRate, detections, follow, setFollow, can } = useDrone();
  const telemetry = useMemo(() => toTelemetry(snapshot), [snapshot]);
  const prefix = info?.stream_prefix;

  const cameras = useCameras(id, prefix);
  const gimbal = useGimbal(id, sendGimbalRate, null, notify);
  const recording = useRecording(id, prefix ?? id, notify);

  const [feedLinks, setFeedLinks] = useState<Record<string, LinkState>>({});
  const [libraryOpen, setLibraryOpen] = useState(false);
  const [previewItem, setPreviewItem] = useState<Recording | null>(null);
  const [tab, setTab] = useState<SidebarTab>("flight");
  const speaker = useSpeaker(id, tab === "speaker");
  const aircraft = useAircraftFeatures(id);
  const hasThermal = THERMAL_FEATURES.some(aircraft.has);
  const hasAircraft = aircraft.has("obstacle_avoidance") || aircraft.has("rtk") || aircraft.has("health");
  const tabs: SidebarTab[] = [
    "flight",
    "recordings",
    ...(can("ai") ? (["ai"] as const) : []),
    ...(speaker.available ? (["speaker"] as const) : []),
    ...(aircraft.available && hasThermal ? (["thermal"] as const) : []),
    ...(aircraft.available && hasAircraft ? (["aircraft"] as const) : []),
  ];
  const shownTab: SidebarTab = tabs.includes(tab) ? tab : "flight";
  const [gotoActive, setGotoActive] = useState(false);
  const [gotoTarget, setGotoTarget] = useState<[number, number] | null>(null);
  const [mainStream, setMainStream] = useState<string | undefined>(undefined);
  const [openEvent, setOpenEvent] = useState<AIEvent | null>(null);

  const aiStatus = useAIStatus(id);
  const aiEvents = useAIEvents();
  const canFollow = canOperate && Boolean(aiStatus.status?.features.follow);

  const hasGimbal = can("gimbal");
  const hasZoom = can("zoom");
  useKeyboardShortcuts({
    gimbal,
    toggleRecording: recording.toggle,
    enabled: canOperate,
    gimbalEnabled: hasGimbal,
    zoomEnabled: hasZoom,
  });

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      if (openEvent) setOpenEvent(null);
      else if (previewItem) setPreviewItem(null);
      else if (gotoActive) setGotoActive(false);
      else setLibraryOpen(false);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [previewItem, gotoActive, openEvent]);

  const handleFeedPlaying = useCallback((stream: string, playing: boolean) => {
    const next: LinkState = playing ? "ok" : "bad";
    setFeedLinks((links) => (links[stream] === next ? links : { ...links, [stream]: next }));
  }, []);

  const handleCameraSource = useCallback(
    async (source: CameraSource) => {
      try {
        await api.cameraSource(id, source);
        notify(`Switching to the ${source} lens`);
      } catch (error) {
        notify(errorText(error));
      }
    },
    [id, notify],
  );

  const handleGoto = useCallback(
    async (lat: number, lon: number) => {
      setGotoActive(false);
      setGotoTarget([lat, lon]);
      const alt = Math.max(5, Math.round(snapshot?.position?.alt_rel_m ?? 0));
      const ok = await confirm.ask({
        title: "Fly here?",
        message: (
          <>
            The drone switches to GUIDED and flies to <strong>{lat.toFixed(6)}, {lon.toFixed(6)}</strong> at{" "}
            <strong>{alt} m</strong> above home (its current altitude).
          </>
        ),
        confirmLabel: "Go",
        danger: true,
      });
      if (!ok) {
        setGotoTarget(null);
        return;
      }
      try {
        await api.goto(id, lat, lon, alt);
        notify("Flying to target");
      } catch (error) {
        notify(errorText(error));
        setGotoTarget(null);
      }
    },
    [confirm, id, notify, snapshot?.position?.alt_rel_m],
  );

  const handleFollow = useCallback(
    async (stream: string, detection: Detection) => {
      try {
        await api.aiFollow(id, stream, detection.track_id);
        setFollow({ active: true, stream, track_id: detection.track_id });
        notify(`Following ${labelName(detection.label).toLowerCase()} #${detection.track_id}`);
      } catch (error) {
        notify(errorText(error));
      }
    },
    [id, notify, setFollow],
  );

  const handleStopFollow = useCallback(async () => {
    try {
      await api.aiStopFollow(id);
      setFollow({ ...follow, active: false, reason: "stopped" });
    } catch (error) {
      notify(errorText(error));
    }
  }, [follow, id, notify, setFollow]);

  const videoAI: VideoAI = {
    detections,
    follow,
    onFollow: canFollow ? (stream, detection) => void handleFollow(stream, detection) : undefined,
    onStopFollow: canOperate ? () => void handleStopFollow() : undefined,
  };

  const mapCutoff = Date.now() - MAP_EVENT_WINDOW_MS;
  const mapEvents = aiEvents.filter((e) => e.geo && Date.parse(e.at) >= mapCutoff);

  const feedStatuses = (cameras?.feeds ?? []).map((feed) => ({
    label: feed.label,
    state: feedLinks[feed.stream] ?? ("checking" as const),
  }));
  const recordingHandlers = { onPreview: setPreviewItem, onDelete: recording.deleteRecording };
  const canGoto = canOperate && can("goto") && Boolean(snapshot?.online && snapshot.armed);

  // Spot and area temperatures on the video, while the thermal lens is shown
  // and the readings are fresh (none while the aircraft is offline).
  const thermal = aircraft.state?.thermal ?? null;
  const measuring = aircraft.live && aircraft.has("thermal_measure") && thermal !== null && thermal.measure !== "off";
  const onThermalLens = telemetry.camera_source === "thermal";
  const sendAircraft = aircraft.send;
  const meter = useCallback(
    async (command: AircraftCommand) => {
      const reason = await sendAircraft(command);
      if (reason) notify(reason);
    },
    [sendAircraft, notify],
  );
  const renderOverlay =
    measuring && onThermalLens
      ? (videoSize: { width: number; height: number } | null) => (
          <Suspense fallback={null}>
            <ThermalOverlay
              videoSize={videoSize}
              measure={thermal.measure}
              spot={thermal.spot}
              area={thermal.area}
              interactive={canOperate}
              onSpot={(x, y) => void meter({ kind: "meter_spot", x, y })}
              onArea={(x, y, width, height) => void meter({ kind: "meter_area", x, y, width, height })}
            />
          </Suspense>
        )
      : undefined;

  return (
    <PageFrame header={<DroneTopBar extraLinks={feedStatuses} />}>
      <ThermalFilter />
      <Panel className={styles.sidebar}>
        <StatsPanel stats={buildStats(telemetry, gimbal.zoom)} />
        <div className={styles.lower}>
          <div className={`${styles.tabs} ${tabs.length > 3 ? styles.tabsMany : ""}`} role="tablist">
            {tabs.map((t) => (
              <button
                key={t}
                type="button"
                role="tab"
                aria-selected={shownTab === t}
                className={`${styles.tab} ${shownTab === t ? styles.tabActive : ""}`}
                onClick={() => setTab(t)}
              >
                {TAB_LABELS[t]}
              </button>
            ))}
          </div>
          {shownTab === "flight" ? (
            <div className={styles.flight}>
              <FlightPanel notify={notify} confirm={confirm} />
            </div>
          ) : shownTab === "ai" ? (
            <AIPanel
              ai={aiStatus}
              events={aiEvents}
              mainStream={mainStream}
              canOperate={canOperate}
              notify={notify}
              onOpenEvent={setOpenEvent}
            />
          ) : shownTab === "speaker" ? (
            <SpeakerPanel droneId={id} speaker={speaker} canOperate={canOperate} notify={notify} />
          ) : shownTab === "thermal" || shownTab === "aircraft" ? (
            <Suspense fallback={null}>
              {shownTab === "thermal" ? (
                <ThermalPanel
                  aircraft={aircraft}
                  canOperate={canOperate}
                  cameraSource={telemetry.camera_source ?? null}
                  onThermalLens={() => void handleCameraSource("thermal")}
                  notify={notify}
                />
              ) : (
                <AircraftPanel droneId={id} aircraft={aircraft} canOperate={canOperate} confirm={confirm.ask} notify={notify} />
              )}
            </Suspense>
          ) : (
            <RecentRecordings
              listVersion={recording.listVersion}
              onSeeAll={() => setLibraryOpen(true)}
              {...recordingHandlers}
            />
          )}
        </div>
      </Panel>

      <div className={styles.column}>
        <VideoStage
          go2rtcUrl={cameras?.go2rtcUrl ?? ""}
          feeds={cameras?.feeds ?? []}
          telemetry={telemetry}
          gimbal={gimbal}
          recording={recording}
          canOperate={canOperate}
          onFeedPlayingChange={handleFeedPlaying}
          onOpenLibrary={() => setLibraryOpen(true)}
          onCameraSource={handleCameraSource}
          nativeThermal={aircraft.has("thermal_palette")}
          renderOverlay={renderOverlay}
          ai={videoAI}
          onMainStreamChange={setMainStream}
          gimbalControls={hasGimbal}
          zoomControls={hasZoom}
        />
        <div className={styles.bottomRow}>
          <Compass heading={telemetry.heading_deg} />
          <DroneMap
            telemetry={telemetry}
            onGoto={handleGoto}
            gotoActive={gotoActive}
            onToggleGoto={canGoto ? () => setGotoActive((v) => !v) : undefined}
            target={gotoTarget}
            events={mapEvents}
          />
          {hasGimbal && <GimbalPad move={gimbal.move} stop={gimbal.stop} center={gimbal.center} disabled={!canOperate} />}
        </div>
      </div>

      {libraryOpen && (
        <RecordingsLibraryModal
          listVersion={recording.listVersion}
          onClose={() => setLibraryOpen(false)}
          {...recordingHandlers}
        />
      )}
      {previewItem && <RecordingPreviewModal item={previewItem} onClose={() => setPreviewItem(null)} />}
      {openEvent && <EventModal event={openEvent} onClose={() => setOpenEvent(null)} />}
      {confirm.element}
      <Toast message={message} />
    </PageFrame>
  );
}
