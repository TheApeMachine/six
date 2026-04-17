
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import path from "path";
import type { Plugin } from "vite";
import { defineConfig, loadEnv } from "vite";
import { WebSocket, WebSocketServer } from "ws";

const telemetryBridge = (): Plugin => {
	/*
	The Vite dev server runs out of visualizer/ but the canonical config
	(with the programs: block the DSL inspector reads) lives one level up
	in the repo root at cmd/cfg/config.yml. The previous resolve pointed
	at visualizer/cmd/cfg/config.yml — a path that does not exist — so
	every /api/programs request fell through to ENOENT and the viewer
	drawer rendered "HTTP 500". Keep the SIX_CONFIG_PATH escape hatch so
	out-of-tree setups can still point to a bespoke file.
	*/
	let wss: WebSocketServer;

	const broadcastFromProducer = (from: WebSocket, frame: Buffer) => {
		const copy = Buffer.from(frame);

		for (const client of wss.clients) {
			if (client === from) {
				continue;
			}
			if (client.readyState === WebSocket.OPEN) client.send(copy);
		}
	}

	return {
		name: "six-telemetry-bridge",
		configureServer(server) {
			wss = new WebSocketServer({ noServer: true });

			wss.on("connection", (client) => {
				client.on("message", (data, isBinary) => {
					if (!isBinary) {
						return;
					}
					const buf = Buffer.isBuffer(data)
						? data
						: Buffer.from(data as ArrayBuffer);
					broadcastFromProducer(client, buf);
				});
				client.on("close", () => {
					wss.clients.clear();
				});
			});

			server.httpServer?.on("upgrade", (req, socket, head) => {
				if (req.url !== "/ws") return;
				wss.handleUpgrade(req, socket, head, (client) => {
					wss.emit("connection", client, req);
				});
			});
		},
		buildEnd() {
			wss?.close();
		},
	};
}

export default defineConfig(({ mode }) => {
	const env = loadEnv(mode, ".", "");
	return {
		plugins: [react(), tailwindcss(), telemetryBridge()],
		define: {
			"process.env.GEMINI_API_KEY": JSON.stringify(env.GEMINI_API_KEY),
		},
		resolve: {
			alias: {
				"@": path.resolve(__dirname, "./src"),
			},
		},
		server: {
			port: 3000,
			host: "0.0.0.0",
			hmr: process.env.DISABLE_HMR !== "true",
		},
		build: {
			target: "esnext",
			chunkSizeWarningLimit: 600,
			rollupOptions: {
				output: {
					manualChunks: {
						three: ["three"],
						react: ["react", "react-dom"],
					},
				},
			},
		},
	};
});
