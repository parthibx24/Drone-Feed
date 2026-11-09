import calendarIcon from "../../assets/icons/calendar.svg";
import timeIcon from "../../assets/icons/time.svg";
import { useRecordingsList } from "../../hooks/useRecordingsList";
import type { Recording, RecordingSort } from "../../types/recording";
import { formatRecordingDate, formatRecordingTime } from "../../utils/format";
import { PanelTitle } from "../ui/Panel";
import { RecordingActions, type RecordingActionHandlers } from "./RecordingActions";
import styles from "./RecentRecordings.module.css";

const NEWEST_FIRST: RecordingSort = { sortBy: "time", order: "desc" };
const VISIBLE_RECORDINGS = 3;

interface RecordingRowProps extends RecordingActionHandlers {
  item: Recording;
}

function RecordingRow({ item, onPreview, onDelete }: RecordingRowProps) {
  return (
    <div className={styles.row}>
      <div className={styles.info}>
        <p className={styles.filename} title={item.filename}>
          {item.filename}
        </p>
        <div className={styles.meta}>
          <span className={styles.metaItem}>
            <img className={styles.metaIcon} src={calendarIcon} alt="" />
            {formatRecordingDate(item.created_at)}
          </span>
          <span className={styles.metaItem}>
            <img className={styles.metaIcon} src={timeIcon} alt="" />
            {formatRecordingTime(item.created_at)}
          </span>
        </div>
      </div>
      <RecordingActions item={item} onPreview={onPreview} onDelete={onDelete} />
    </div>
  );
}

interface RecentRecordingsProps extends RecordingActionHandlers {
  listVersion: number;
  onSeeAll: () => void;
}

export function RecentRecordings({ listVersion, onSeeAll, onPreview, onDelete }: RecentRecordingsProps) {
  const { items, error, loading } = useRecordingsList(NEWEST_FIRST, listVersion);
  const recent = items.slice(0, VISIBLE_RECORDINGS);

  return (
    <div className={styles.section}>
      <div className={styles.header}>
        <PanelTitle>Recent Recordings</PanelTitle>
        <button type="button" className={styles.seeAll} onClick={onSeeAll}>
          See All
        </button>
      </div>
      <div className={styles.list}>
        {recent.map((item) => (
          <RecordingRow key={item.relative_path} item={item} onPreview={onPreview} onDelete={onDelete} />
        ))}
        {recent.length === 0 && (
          <p className={styles.empty}>
            {error ? `Failed to load recordings: ${error}` : loading ? "Loading recordings…" : "No recordings yet."}
          </p>
        )}
      </div>
    </div>
  );
}
