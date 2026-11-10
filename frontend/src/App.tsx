import { BrowserRouter, Navigate, Outlet, Route, Routes, useLocation, useParams } from "react-router";
import { AuthProvider, useAuth } from "./context/AuthContext";
import { DroneProvider } from "./context/DroneContext";
import { Dashboard } from "./pages/Dashboard/Dashboard";
import { FleetPage } from "./pages/Fleet/FleetPage";
import { LoginPage } from "./pages/Login/LoginPage";
import { MessagesPage } from "./pages/Messages/MessagesPage";
import { MissionPage } from "./pages/Mission/MissionPage";
import { ParamsPage } from "./pages/Params/ParamsPage";

/** Sends logged-out visitors to /login and remembers where they were going. */
function RequireAuth() {
  const { me } = useAuth();
  const location = useLocation();
  if (me === undefined) return null; // checking session
  if (me === null) return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />;
  return <Outlet />;
}

/** All /drones/:id pages share one live connection to that drone. */
function DroneRoute() {
  const { id = "" } = useParams();
  return (
    <DroneProvider key={id} id={id}>
      <Outlet />
    </DroneProvider>
  );
}

export function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route element={<RequireAuth />}>
            <Route index element={<FleetPage />} />
            <Route path="drones/:id" element={<DroneRoute />}>
              <Route index element={<Dashboard />} />
              <Route path="mission" element={<MissionPage />} />
              <Route path="params" element={<ParamsPage />} />
              <Route path="messages" element={<MessagesPage />} />
            </Route>
          </Route>
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  );
}
