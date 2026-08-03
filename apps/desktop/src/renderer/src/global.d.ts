import type { DesktopApi, DesktopMinimumVersionApi } from "@preload/types";

declare global {
  interface Window {
    tutti?: DesktopApi;
    tuttiMinimumVersion?: DesktopMinimumVersionApi;
  }

  interface ImportMetaEnv {
    readonly VITE_TUTTID_ACCESS_TOKEN?: string;
    readonly VITE_TUTTID_BASE_URL?: string;
    readonly VITE_TUTTI_REACT_PROFILER?: string;
    readonly VITE_TUTTI_WHY_DID_YOU_RENDER?: string;
    readonly VITE_TUTTI_WEB_DEV?: string;
    readonly VITE_TUTTI_WEB_WORKSPACE_ID?: string;
  }

  interface ImportMeta {
    readonly env: ImportMetaEnv;
  }
}

export {};
