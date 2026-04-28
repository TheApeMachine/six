import { type ChildProcess, spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import path from "path";
import type { Plugin } from "vite";
import { defineConfig } from "vite";

const vizRoot = path.dirname(fileURLToPath(import.meta.url));

/*
sixTelemetryBridgePlugin starts visualizer/server/bridge.ts during "vite" dev
so :6600 is listening before the Go process dials telemetry.ws_url (often
ws://127.0.0.1:3000/ws through the proxy below). Set VITE_SKIP_TELEMETRY_BRIDGE=true
to run the hub manually ("npm run bridge").
*/
function sixTelemetryBridgePlugin(): Plugin {
	let child: ChildProcess | null = null;

	const stop = () => {
		if (child !== null && !child.killed) {
			child.kill("SIGTERM");
			child = null;
		}
	};

	return {
		name: "six-telemetry-bridge",
		configureServer(server) {
			if (process.env.VITE_SKIP_TELEMETRY_BRIDGE === "true") {
				return;
			}

			child = spawn("npm", ["run", "bridge"], {
				cwd: vizRoot,
				env: { ...process.env },
				shell: true,
				stdio: "inherit",
			});

			child.on("error", (err) => {
				console.error("[six-telemetry-bridge]", err);
			});

			server.httpServer?.once("close", stop);
		},
	};
}

export default defineConfig(() => {
	return {
		plugins: [react(), tailwindcss(), sixTelemetryBridgePlugin()],
		resolve: {
			alias: {
				"@": path.resolve(__dirname, "./src"),
			},
		},
		server: {
			port: 3000,
			host: "0.0.0.0",
			hmr: process.env.DISABLE_HMR !== "true",
			proxy: {
				"/ws": {
					target: "ws://127.0.0.1:6600",
					ws: true,
				},
				"/api": {
					target: "http://127.0.0.1:6600",
				},
			},
		},
		build: {
			target: "esnext",
			chunkSizeWarningLimit: 600,
			rollupOptions: {
				output: {
					manualChunks: {
						react: ["react", "react-dom"]
					}
				}
			}
		}
	};
});
