import { useRef, useState } from "react";
import folderIcon from "../../assets/icons/folder.svg";
import fullscreenIcon from "../../assets/icons/fullscreen.svg";
import type { GimbalControls } from "../../hooks/useGimbal";
import type { RecordingControls } from "../../hooks/useRecording";
import type { CameraFeed, Telemetry } from "../../types/drone";
import type { CameraSource } from "../../types/gateway";
import { ControlGroup } from "../ui/ControlGroup";
import { IconButton } from "../ui/IconButton";
import { Panel } from "../ui/Panel";
import { SegmentedControl } from "../ui/SegmentedControl";
import { FlightStatusChips } from "./FlightStatusChips";
import { RecordButton } from "./RecordButton";
import { SpeedControl } from "./SpeedControl";
import { VideoFeed } from "./VideoFeed";
import { ZoomControl } from "./ZoomControl";
import styles from "./VideoStage.module.css";

interface VideoStageProps {
  go2rtcUrl: string;
  feeds: CameraFeed[];
  telemetry: Telemetry;
  gimbal: GimbalControls;
  recording: RecordingControls;
  /** False for viewers: gimbal, zoom and recording controls are disabled. */
  canOperate?: boolean;
  onFeedPlayingChange: (stream: string, playing: boolean) => void;
  onOpenLibrary: () => void;
  /** Switches the lens, on cameras that report one (telemetry.camera_source). */
  onCameraSource?: (source: CameraSource) => void;
}

const SOURCES: { value: CameraSource; label: string }[] = [
  { value: "wide", label: "Wide" },
  { value: "zoom", label: "Zoom" },
  { value: "thermal", label: "Thermal" },
];

export function VideoStage({
  go2rtcUrl,
  feeds,
  telemetry,
  gimbal,
  recording,
  canOperate = true,
  onFeedPlayingChange,
  onOpenLibrary,
  onCameraSource,
}: VideoStageProps) {
  // A camera that switches lenses sends one stream showing the chosen lens
  // (Mavic 3T): the selector picks the lens, not the stream, and the feed
  // gets the thermal palette while the thermal lens is on.
  const source = telemetry.camera_source ?? null;
  const shownFeeds = source ? feeds.map((feed) => ({ ...feed, thermal: source === "thermal" })) : feeds;
  const [selected, setSelected] = useState<string | null>(null);
  const mainFeedRef = useRef<HTMLDivElement>(null);
  const mainStream = feeds.some((feed) => feed.stream === selected) ? selected! : feeds[0]?.stream;

  const enterFullscreen = () => {
    mainFeedRef.current?.requestFullscreen().catch((error) => console.warn("Fullscreen failed:", error));
  };

  return (
    <Panel className={styles.stage}>
      {shownFeeds.map((feed) => (
        <VideoFeed
          key={feed.stream}
          feed={feed}
          go2rtcUrl={go2rtcUrl}
          isMain={feed.stream === mainStream}
          rootRef={feed.stream === mainStream ? mainFeedRef : undefined}
          onPlayingChange={onFeedPlayingChange}
          onSelect={setSelected}
        />
      ))}

      <div className={styles.topControls}>
        {source && onCameraSource ? (
          <SegmentedControl
            ariaLabel="Camera lens"
            options={SOURCES}
            value={source}
            onChange={(value) => value !== source && onCameraSource(value as CameraSource)}
            disabled={!canOperate}
          />
        ) : (
          <SegmentedControl
            ariaLabel="Camera"
            options={feeds.map((feed) => ({ value: feed.stream, label: feed.label }))}
            value={mainStream ?? ""}
            onChange={setSelected}
          />
        )}
        <FlightStatusChips telemetry={telemetry} />
        <div className={styles.recordControls}>
          <ControlGroup className={styles.iconTray}>
            <IconButton icon={folderIcon} label="View recordings library" onClick={onOpenLibrary} />
          </ControlGroup>
          <RecordButton recording={recording} disabled={!canOperate} />
        </div>
      </div>

      <div className={styles.bottomControls}>
        <ControlGroup className={styles.iconTray}>
          <IconButton icon={fullscreenIcon} label="Full screen" onClick={enterFullscreen} />
        </ControlGroup>
        <SpeedControl speed={gimbal.speed} setSpeed={gimbal.setSpeed} />
        <ZoomControl zoom={gimbal.zoom} zoomStep={gimbal.zoomStep} zoomReset={gimbal.zoomReset} disabled={!canOperate} />
      </div>
    </Panel>
  );
}
