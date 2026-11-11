import type { AIEvent } from "../../types/ai";
import { formatDistance, formatEventDateTime, labelName, mapsUrl, percent, streamLabel } from "../../utils/ai";
import { Modal } from "../ui/Modal";
import styles from "./EventDetails.module.css";

/** Snapshot and facts of one AI event; used in the modal and on the Detections page. */
export function EventDetails({ event }: { event: AIEvent }) {
  return (
    <div className={styles.details}>
      <a className={styles.imageLink} href={event.snapshot_url} target="_blank" rel="noreferrer" title="Open full size">
        <img className={styles.image} src={event.snapshot_url} alt={`${labelName(event.label)} detected by ${event.drone_name}`} />
      </a>
      <dl className={styles.facts}>
        <dt>Detected</dt>
        <dd>
          {labelName(event.label)} · {percent(event.confidence)} confidence
        </dd>
        <dt>When</dt>
        <dd>{formatEventDateTime(event.at)}</dd>
        <dt>Drone</dt>
        <dd>
          {event.drone_name || event.drone_id} · {streamLabel(event.stream)} camera · track #{event.track_id}
        </dd>
        {event.drone && (
          <>
            <dt>Drone position</dt>
            <dd>
              {event.drone.lat.toFixed(6)}, {event.drone.lon.toFixed(6)} · {Math.round(event.drone.alt_rel_m)} m above home
            </dd>
          </>
        )}
        <dt>Object position</dt>
        <dd>
          {event.geo ? (
            <>
              {event.geo.lat.toFixed(6)}, {event.geo.lon.toFixed(6)} · {formatDistance(event.geo.distance_m)} from the drone
              {event.geo.approx && <span className={styles.muted}> (estimate)</span>} ·{" "}
              <a href={mapsUrl(event.geo.lat, event.geo.lon)} target="_blank" rel="noreferrer">
                Open in Google Maps
              </a>
            </>
          ) : (
            <span className={styles.muted}>Unknown (gimbal angle not known, or the camera looked at the horizon)</span>
          )}
        </dd>
        {(event.alerted || event.recording) && (
          <>
            <dt>Actions</dt>
            <dd>
              {[event.alerted && "Alert sent", event.recording && "Recording started"].filter(Boolean).join(" · ")}
            </dd>
          </>
        )}
      </dl>
    </div>
  );
}

export function EventModal({ event, onClose }: { event: AIEvent; onClose: () => void }) {
  return (
    <Modal title={`${labelName(event.label)} · ${percent(event.confidence)}`} onClose={onClose}>
      <EventDetails event={event} />
    </Modal>
  );
}
