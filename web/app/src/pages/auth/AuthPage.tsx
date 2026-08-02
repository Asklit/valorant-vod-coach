import { AlertTriangle, Crosshair, Shield } from "lucide-react";

type AuthPageProps = {
  email: string;
  mode: "login" | "register";
  name: string;
  password: string;
  setupToken: string;
  error: string;
  setupRequired: boolean;
  onEmailChange: (value: string) => void;
  onModeChange: (value: "login" | "register") => void;
  onNameChange: (value: string) => void;
  onPasswordChange: (value: string) => void;
  onSetupTokenChange: (value: string) => void;
  onSubmit: () => void;
};

export function AuthPage(props: AuthPageProps) {
  return (
    <main className="auth-shell">
      <section className="auth-panel">
        <div className="brand-lockup">
          <div className="brand-mark"><Crosshair size={24} /></div>
          <div>
            <div className="brand-title">VOD COACH</div>
            <div className="brand-subtitle">EVIDENCE REVIEW</div>
          </div>
        </div>

        <div className="auth-mode" role="tablist">
          <button aria-selected={props.mode === "login"} className={props.mode === "login" ? "active" : ""} onClick={() => props.onModeChange("login")} role="tab" type="button">Sign in</button>
          <button aria-selected={props.mode === "register"} className={props.mode === "register" ? "active" : ""} onClick={() => props.onModeChange("register")} role="tab" type="button">Register</button>
        </div>

        <form onSubmit={(event) => { event.preventDefault(); props.onSubmit(); }}>
          {props.mode === "register" && (
            <>
              <label>
                <span>Name</span>
                <input autoComplete="name" onChange={(event) => props.onNameChange(event.target.value)} placeholder="Player name" required value={props.name} />
              </label>
              {props.setupRequired && (
                <label>
                  <span>Installation key</span>
                  <input autoComplete="off" onChange={(event) => props.onSetupTokenChange(event.target.value)} placeholder="Administrator setup token" required type="password" value={props.setupToken} />
                </label>
              )}
            </>
          )}
          <label>
            <span>Email</span>
            <input autoComplete="email" onChange={(event) => props.onEmailChange(event.target.value)} placeholder="player@example.com" required type="email" value={props.email} />
          </label>
          <label>
            <span>Password</span>
            <input autoComplete={props.mode === "login" ? "current-password" : "new-password"} minLength={8} onChange={(event) => props.onPasswordChange(event.target.value)} placeholder="minimum 8 characters" required type="password" value={props.password} />
          </label>
          {props.error && <div className="auth-error" role="alert"><AlertTriangle size={16} />{props.error}</div>}
          <button className="auth-submit" type="submit"><Shield size={17} />{props.mode === "login" ? "Sign in" : "Create account"}</button>
        </form>
      </section>
    </main>
  );
}
