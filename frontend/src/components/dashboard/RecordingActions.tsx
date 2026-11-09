import deleteIcon from "../../assets/icons/delete.svg";
import downloadIcon from "../../assets/icons/download.svg";
import playIcon from "../../assets/icons/play.svg";
import type { Recording } from "../../types/recording";
import { IconButton, IconLink } from "../ui/IconButton";
import styles from "./RecordingActions.module.css";

export interface RecordingActionHandlers {
  onPreview: (item: Recording) => void;
  onDelete: (item: Recording) => void;
}

interface RecordingActionsProps extends RecordingActionHandlers {
  item: Recording;
}

export function RecordingActions({ item, onPreview, onDelete }: RecordingActionsProps) {
  return (
    <div className={styles.actions}>
      <IconButton
        icon={playIcon}
        label="Preview"
        variant="tile"
        disabled={item.is_active}
        onClick={() => onPreview(item)}
      />
      <IconLink
        icon={downloadIcon}
        label="Download"
        variant="tile"
        href={item.download_url}
        download={item.filename}
      />
      <IconButton
        icon={deleteIcon}
        label="Delete"
        variant="tile"
        disabled={item.is_active}
        onClick={() => onDelete(item)}
      />
    </div>
  );
}
