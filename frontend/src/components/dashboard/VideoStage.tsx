import { useRef, useState } from "react";
import folderIcon from "../../assets/icons/folder.svg";
import fullscreenIcon from "../../assets/icons/fullscreen.svg";
import type { GimbalControls } from "../../hooks/useGimbal";
import type { RecordingControls } from "../../hooks/useRecording";
import type { CameraFeed, Telemetry } from "../../types/drone";
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
}

export function VideoStage({
  go2rtcUrl,
  feeds,
  telemetry,
  gimbal,
  recording,
  canOperate = true,
  onFeedPlayingChange,
  onOpenLibrary,
}: VideoStageProps) {
  const [selected, setSelected] = useState<string | null>(null);
  const mainFeedRef = useRef<HTMLDivElement>(null);
  const mainStream = feeds.some((feed) => feed.stream === selected) ? selected! : feeds[0]?.stream;

  const enterFullscreen = () => {
    mainFeedRef.current?.requestFullscreen().catch((error) => console.warn("Fullscreen failed:", error));
  };

  return (
    <Panel className={styles.stage}>
      {feeds.map((feed) => (
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
        <SegmentedControl
          ariaLabel="Camera"
          options={feeds.map((feed) => ({ value: feed.stream, label: feed.label }))}
          value={mainStream ?? ""}
          onChange={setSelected}
        />
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
