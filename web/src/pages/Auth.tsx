import { useMutation, useQueryClient } from "@tanstack/react-query";
import { KeyRound, ShieldCheck } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router";
import { AuthShell } from "../components/AuthShell";
import { Alert, Button, Card, Field, Input } from "../components/ui";
import { api } from "../lib/api";
import { useI18n } from "../lib/i18n";

export function RegisterPage() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [touched, setTouched] = useState(false);
  const mismatch = touched && confirm.length > 0 && password !== confirm;

  const register = useMutation({
    mutationFn: () => api.register(username.trim(), password),
    onSuccess: async (user) => {
      qc.setQueryData(["me"], user);
      await qc.invalidateQueries({ queryKey: ["setup"] });
      navigate("/onboarding", { replace: true });
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (password !== confirm) return;
    register.mutate();
  };

  return (
    <AuthShell>
      <Card className="animate-fade-in p-8">
        <div className="mb-6 flex size-11 items-center justify-center rounded-xl bg-accent-soft text-accent">
          <ShieldCheck className="size-5" />
        </div>
        <h1 className="text-2xl font-semibold tracking-tight">{t("auth.registerTitle")}</h1>
        <p className="mt-1.5 text-sm text-muted">{t("auth.registerSubtitle")}</p>
        <form onSubmit={submit} className="mt-6 space-y-4">
          <Field label={t("auth.username")} htmlFor="username">
            <Input id="username" autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required minLength={3} />
          </Field>
          <Field label={t("auth.password")} htmlFor="password" hint={t("auth.passwordHint")}>
            <Input
              id="password"
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              minLength={8}
              autoFocus
            />
          </Field>
          <Field label={t("auth.confirmPassword")} htmlFor="confirm" error={mismatch ? t("auth.passwordMismatch") : undefined}>
            <Input
              id="confirm"
              type="password"
              autoComplete="new-password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              onBlur={() => setTouched(true)}
              invalid={mismatch}
              required
            />
          </Field>
          {register.error && <Alert tone="danger">{register.error.message}</Alert>}
          <Button type="submit" size="lg" className="w-full" loading={register.isPending}>
            {t("auth.registerButton")}
          </Button>
        </form>
      </Card>
    </AuthShell>
  );
}

export function LoginPage() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const login = useMutation({
    mutationFn: () => api.login(username.trim(), password),
    onSuccess: (user) => {
      qc.setQueryData(["me"], user);
      navigate("/", { replace: true });
    },
  });
  return (
    <AuthShell>
      <Card className="animate-fade-in p-8">
        <div className="mb-6 flex size-11 items-center justify-center rounded-xl bg-accent-soft text-accent">
          <KeyRound className="size-5" />
        </div>
        <h1 className="text-2xl font-semibold tracking-tight">{t("auth.loginTitle")}</h1>
        <p className="mt-1.5 text-sm text-muted">{t("auth.loginSubtitle")}</p>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            login.mutate();
          }}
          className="mt-6 space-y-4"
        >
          <Field label={t("auth.username")} htmlFor="username">
            <Input id="username" autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required autoFocus />
          </Field>
          <Field label={t("auth.password")} htmlFor="password">
            <Input
              id="password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </Field>
          {login.error && <Alert tone="danger">{login.error.message}</Alert>}
          <Button type="submit" size="lg" className="w-full" loading={login.isPending}>
            {t("auth.loginButton")}
          </Button>
        </form>
      </Card>
    </AuthShell>
  );
}
