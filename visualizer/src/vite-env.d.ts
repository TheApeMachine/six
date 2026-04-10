/// <reference types="vite/client" />

/*
Module augmentation for optional WS endpoint env vars (merged into ImportMetaEnv).
*/
declare module "vite/client" {
	interface ImportMetaEnv {
		readonly VITE_VIZ_HOST?: string;
		readonly VITE_VIZ_PORT?: string;
	}
}
