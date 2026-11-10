import { useState, type FormEvent } from "react";
import { Navigate, useLocation, useNavigate } from "react-router";
import logo from "../../assets/brand/logo.png";
import { Button } from "../../components/ui/Button";
import { Field, TextInput } from "../../components/ui/Field";
import { Panel } from "../../components/ui/Panel";
import { useAuth } from "../../context/AuthContext";
import { ApiError } from "../../services/http";
import styles from "./LoginPage.module.css";

export function LoginPage() {
  const { me, login } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const from = (location.state as { from?: string } | null)?.from ?? "/";
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  if (me) return <Navigate to={from} replace />;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await login(username.trim(), password);
      navigate(from, { replace: true });
    } catch (err) {
      setError(err instanceof ApiError && err.status === 429 ? err.message : "Wrong username or password");
      setPassword("");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className={styles.page}>
      <Panel className={styles.card}>
        <div className={styles.logoFrame}>
          <img className={styles.logo} src={logo} alt="StarWatch" />
        </div>
        <h1 className={styles.title}>Sign in</h1>
        <form className={styles.form} onSubmit={submit}>
          <Field label="Username">
            <TextInput autoFocus autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required />
          </Field>
          <Field label="Password">
            <TextInput
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </Field>
          {error && <p className={styles.error}>{error}</p>}
          <Button type="submit" variant="primary" busy={busy} className={styles.submit}>
            Sign in
          </Button>
        </form>
      </Panel>
    </div>
  );
}
