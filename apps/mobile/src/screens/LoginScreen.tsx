import { useServiceSnapshot } from "../bindings/useServiceSnapshot";
import type { LoginService } from "../services/loginService";
import { LoginScreenView } from "./LoginScreenView";

export function LoginScreen({ service }: { service: LoginService }) {
  const model = useServiceSnapshot(service);
  return (
    <LoginScreenView model={model} onLogin={() => void service.submitLogin()} />
  );
}
