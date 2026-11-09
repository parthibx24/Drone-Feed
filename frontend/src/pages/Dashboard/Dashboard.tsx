import { useCallback, useEffect, useMemo, useState } from "react";
import { Compass } from "../../components/dashboard/Compass";
import { DroneMap } from "../../components/dashboard/DroneMap";
import { GimbalPad } from "../../components/dashboard/GimbalPad";
import { RecentRecordings } from "../../components/dashboard/RecentRecordings";
import { RecordingPreviewModal } from "../../components/dashboard/RecordingPreviewModal";
import { RecordingsLibraryModal } from "../../components/dashboard/RecordingsLibraryModal";
import { StatsPanel } from "../../components/dashboard/StatsPanel";
import { ThermalFilter } from "../../components/dashboard/ThermalFilter";
import { TopBar, type LinkStatus } from "../../components/dashboard/TopBar";
import { VideoStage } from "../../components/dashboard/VideoStage";
import { Panel } from "../../components/ui/Panel";
import { Toast } from "../../components/ui/Toast";
import { useCameras } from "../../hooks/useCameras";
import { useGimbal } from "../../hooks/useGimbal";
import { useKeyboardShortcuts } from "../../hooks/useKeyboardShortcuts";
import { useRecording } from "../../hooks/useRecording";
import { useTelemetry } from "../../hooks/useTelemetry";
import { useToast } from "../../hooks/useToast";
import type { DroneParams, LinkState } from "../../types/drone";
import type { Recording } from "../../types/recording";
import { buildStats } from "../../utils/format";
import styles from "./Dashboard.module.css";

export function Dashboard({ drone }: { drone: DroneParams }) {
  const { message, notify } = useToast();
  const { telemetry, agentLink, mavlinkLink } = useTelemetry(drone.host);
  const cameras = useCameras(drone.name);
  const gimbal = useGimbal(drone.host, telemetry.zoom_level ?? telemetry.zoom ?? null, notify);
  const recording = useRecording(drone.name, notify);

  const [feedLinks, setFeedLinks] = useState<Record<string, LinkState>>({});
  const [libraryOpen, setLibraryOpen] = useState(false);
  const [previewItem, setPreviewItem] = useState<Recording | null>(null);

  useKeyboardShortcuts({ gimbal, toggleRecording: recording.toggle });

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      if (previewItem) setPreviewItem(null);
      else setLibraryOpen(false);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [previewItem]);

  const handleFeedPlaying = useCallback((stream: string, playing: boolean) => {
    const next: LinkState = playing ? "ok" : "bad";
    setFeedLinks((links) => (links[stream] === next ? links : { ...links, [stream]: next }));
  }, []);

  const links = useMemo<LinkStatus[]>(
    () => [
      { label: "Agent", state: agentLink },
      { label: "MAVLink", state: mavlinkLink },
      ...(cameras?.feeds ?? []).map((feed) => ({
        label: feed.label,
        state: feedLinks[feed.stream] ?? ("checking" as const),
      })),
    ],
    [agentLink, mavlinkLink, cameras, feedLinks],
  );

  const recordingHandlers = { onPreview: setPreviewItem, onDelete: recording.deleteRecording };

  return (
    <div className={styles.page}>
      <ThermalFilter />
      <TopBar droneName={drone.name} links={links} />

      <main className={styles.content}>
        <Panel className={styles.sidebar}>
          <StatsPanel stats={buildStats(telemetry, gimbal.zoom)} />
          <RecentRecordings
            listVersion={recording.listVersion}
            onSeeAll={() => setLibraryOpen(true)}
            {...recordingHandlers}
          />
        </Panel>

        <div className={styles.column}>
          <VideoStage
            go2rtcUrl={cameras?.go2rtcUrl ?? ""}
            feeds={cameras?.feeds ?? []}
            telemetry={telemetry}
            gimbal={gimbal}
            recording={recording}
            onFeedPlayingChange={handleFeedPlaying}
            onOpenLibrary={() => setLibraryOpen(true)}
          />
          <div className={styles.bottomRow}>
            <Compass heading={telemetry.heading_deg} />
            <DroneMap telemetry={telemetry} />
            <GimbalPad move={gimbal.move} stop={gimbal.stop} center={gimbal.center} />
          </div>
        </div>
      </main>

      {libraryOpen && (
        <RecordingsLibraryModal
          listVersion={recording.listVersion}
          onClose={() => setLibraryOpen(false)}
          {...recordingHandlers}
        />
      )}
      {previewItem && <RecordingPreviewModal item={previewItem} onClose={() => setPreviewItem(null)} />}
      <Toast message={message} />
    </div>
  );
}
