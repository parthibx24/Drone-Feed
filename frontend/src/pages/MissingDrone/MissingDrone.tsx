import { Panel, PanelTitle } from "../../components/ui/Panel";
import styles from "./MissingDrone.module.css";

export function MissingDrone() {
  return (
    <Panel className={styles.panel}>
      <PanelTitle>Missing ?host= in URL</PanelTitle>
      <p className={styles.hint}>
        Open this page as {window.location.origin}/?host=&lt;DRONE_IP&gt;&amp;name=&lt;DRONE_NAME&gt;
      </p>
    </Panel>
  );
}
