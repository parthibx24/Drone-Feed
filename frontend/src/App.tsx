import { Dashboard } from "./pages/Dashboard/Dashboard";
import { MissingDrone } from "./pages/MissingDrone/MissingDrone";
import { readDroneParams } from "./utils/droneParams";

const drone = readDroneParams(window.location.search);

export function App() {
  return drone ? <Dashboard drone={drone} /> : <MissingDrone />;
}
