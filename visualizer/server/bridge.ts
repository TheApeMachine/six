import http from "node:http";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import express from "express";
import { WebSocketServer } from "ws";
import YAML from "yaml";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const BRIDGE_HOST = process.env.VIZ_BRIDGE_HOST || "0.0.0.0";
const BRIDGE_PORT = Number(
	process.env.VIZ_BRIDGE_PORT || process.env.VITE_VIZ_PORT || "6600",
);
const CONTROL_URL = (process.env.TELEMETRY_CONTROL_URL || "").trim();
const CONFIG_PATH =
	process.env.SIX_CONFIG_PATH ||
	path.resolve(__dirname, "../../cmd/cfg/config.yml");

interface BridgeDiagnostics {
	clients: number;
}

const diagnostics: BridgeDiagnostics = {
	clients: 0,
};

const app = express();
app.use(express.json({ limit: "1mb" }));

app.get("/", (_req, res) => {
	res.json({
		bridge: "six-visualizer",
		controlUrl: CONTROL_URL || null,
		diagnostics,
		ingest: "websocket",
		path: "/ws",
	});
});

app.get("/api/programs", async (_req, res) => {
	try {
		const source = await readFile(CONFIG_PATH, "utf8");
		const doc = YAML.parse(source) as { programs?: Record<string, unknown> };
		const programs: Record<string, string> = {};

		for (const [name, value] of Object.entries(doc.programs ?? {})) {
			if (typeof value === "string" && value.trim() !== "") {
				programs[name] = value;
			}
		}

		res.json(programs);
	} catch (error) {
		res.status(500).json({
			error: error instanceof Error ? error.message : String(error),
		});
	}
});

const server = http.createServer(app);
const websocketServer = new WebSocketServer({ noServer: true });

server.on("upgrade", (request, socket, head) => {
	if (request.url !== "/ws") {
		socket.destroy();
		return;
	}

	websocketServer.handleUpgrade(request, socket, head, (client) => {
		websocketServer.emit("connection", client, request);
	});
});

websocketServer.on("connection", (client) => {
	diagnostics.clients = websocketServer.clients.size;

	client.on("message", (data, isBinary) => {
		for (const peer of websocketServer.clients) {
			if (peer === client) continue;
			if (peer.readyState !== peer.OPEN) continue;
			peer.send(data, { binary: isBinary });
		}
	});

	client.on("close", () => {
		diagnostics.clients = websocketServer.clients.size;
	});
});

server.listen(BRIDGE_PORT, BRIDGE_HOST, () => {
	console.log(
		`[bridge] http/ws listening on http://${BRIDGE_HOST}:${BRIDGE_PORT}`,
	);
	console.log(`[bridge] config path ${CONFIG_PATH}`);
	if (!CONTROL_URL) {
		console.log(
			"[bridge] prompt forwarding disabled (TELEMETRY_CONTROL_URL not set)",
		);
	}
});

function shutdown(signal: string) {
	console.log(`[bridge] shutting down on ${signal}`);
	websocketServer.close();
	server.close(() => {
		process.exit(0);
	});
}

process.on("SIGINT", () => shutdown("SIGINT"));
process.on("SIGTERM", () => shutdown("SIGTERM"));
