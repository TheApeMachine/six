import { readFile } from "node:fs/promises";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import path from "path";
import type { Plugin } from "vite";
import { defineConfig, loadEnv } from "vite";
import { WebSocket, WebSocketServer } from "ws";
import YAML from "yaml";

function telemetryBridge(): Plugin {
	const CONTROL_URL = (process.env.TELEMETRY_CONTROL_URL || "").trim();
	/*
	The Vite dev server runs out of visualizer/ but the canonical config
	(with the programs: block the DSL inspector reads) lives one level up
	in the repo root at cmd/cfg/config.yml. The previous resolve pointed
	at visualizer/cmd/cfg/config.yml — a path that does not exist — so
	every /api/programs request fell through to ENOENT and the viewer
	drawer rendered "HTTP 500". Keep the SIX_CONFIG_PATH escape hatch so
	out-of-tree setups can still point to a bespoke file.
	*/
	const CONFIG_PATH =
		process.env.SIX_CONFIG_PATH ||
		path.resolve(__dirname, "../cmd/cfg/config.yml");
	const FRAME_HISTORY_LIMIT = Number(process.env.VIZ_FRAME_HISTORY || "0");

	let wss: WebSocketServer;
	const frameHistory: Buffer[] = [];

	const diagnostics = {
		bytesReceived: 0,
		clients: 0,
		droppedFrames: 0,
		frameHistory: 0,
		framesReceived: 0,
		lastFrameAt: null as number | null,
	};

	function broadcastFromProducer(from: WebSocket, frame: Buffer) {
		const copy = Buffer.from(frame);

		if (FRAME_HISTORY_LIMIT > 0) {
			frameHistory.push(copy);
			if (frameHistory.length > FRAME_HISTORY_LIMIT) {
				frameHistory.shift();
				diagnostics.droppedFrames += 1;
			}
		}

		diagnostics.framesReceived += 1;
		diagnostics.bytesReceived += copy.byteLength;
		diagnostics.frameHistory = frameHistory.length;
		diagnostics.lastFrameAt = Date.now();

		for (const client of wss.clients) {
			if (client === from) {
				continue;
			}
			if (client.readyState === WebSocket.OPEN) client.send(copy);
		}
	}

	async function loadPrograms() {
		if (CONTROL_URL) {
			const res = await fetch(new URL("/programs", CONTROL_URL), {
				headers: { accept: "application/json" },
			});
			if (!res.ok)
				throw new Error(`control programs request failed: ${res.status}`);
			return (await res.json()) as Record<string, string>;
		}

		const source = await readFile(CONFIG_PATH, "utf8");
		const parsed = YAML.parse(source) as {
			programs?: Record<string, string>;
		} | null;
		return parsed?.programs ?? {};
	}

	async function forwardPrompt(prompt: string) {
		if (!CONTROL_URL)
			throw new Error("prompt control endpoint is not configured");
		const res = await fetch(new URL("/prompt", CONTROL_URL), {
			body: JSON.stringify({ prompt }),
			headers: { "content-type": "application/json" },
			method: "POST",
		});
		const payload = (await res.json()) as unknown;
		if (!res.ok) {
			throw new Error(
				typeof payload === "object" && payload !== null && "error" in payload
					? String((payload as { error: unknown }).error)
					: `prompt request failed: ${res.status}`,
			);
		}
		return payload;
	}

	return {
		name: "six-telemetry-bridge",
		configureServer(server) {
			wss = new WebSocketServer({ noServer: true });

			wss.on("connection", (client) => {
				diagnostics.clients = wss.clients.size;
				if (FRAME_HISTORY_LIMIT > 0) {
					for (const frame of frameHistory) client.send(frame);
				}
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
					diagnostics.clients = wss.clients.size;
				});
			});

			server.httpServer?.on("upgrade", (req, socket, head) => {
				if (req.url !== "/ws") return;
				wss.handleUpgrade(req, socket, head, (client) => {
					wss.emit("connection", client, req);
				});
			});

			server.middlewares.use("/api/diagnostics", (_req, res) => {
				res.setHeader("content-type", "application/json");
				res.end(
					JSON.stringify({
						controlUrl: CONTROL_URL || null,
						diagnostics,
						frameHistoryLimit: FRAME_HISTORY_LIMIT,
						ingest: "websocket",
						path: "/ws",
					}),
				);
			});

			server.middlewares.use("/api/programs", async (_req, res) => {
				try {
					const programs = await loadPrograms();
					res.setHeader("content-type", "application/json");
					res.end(JSON.stringify(programs));
				} catch (err) {
					res.statusCode = 500;
					res.setHeader("content-type", "application/json");
					res.end(
						JSON.stringify({
							error: err instanceof Error ? err.message : String(err),
						}),
					);
				}
			});

			server.middlewares.use("/api/prompt", async (req, res) => {
				if (req.method !== "POST") {
					res.statusCode = 405;
					res.end();
					return;
				}

				const chunks: Buffer[] = [];
				for await (const chunk of req) chunks.push(chunk as Buffer);
				const body = JSON.parse(Buffer.concat(chunks).toString()) as {
					prompt?: string;
				};
				const prompt = body?.prompt?.trim() || "";

				if (!prompt) {
					res.statusCode = 400;
					res.setHeader("content-type", "application/json");
					res.end(JSON.stringify({ error: "prompt is required" }));
					return;
				}

				try {
					const response = await forwardPrompt(prompt);
					res.setHeader("content-type", "application/json");
					res.end(JSON.stringify(response));
				} catch (err) {
					res.statusCode = CONTROL_URL ? 502 : 501;
					res.setHeader("content-type", "application/json");
					res.end(
						JSON.stringify({
							error: err instanceof Error ? err.message : String(err),
						}),
					);
				}
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
