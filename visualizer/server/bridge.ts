import { readFile } from "node:fs/promises";
import http from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";
import express from "express";
import { WebSocket, WebSocketServer } from "ws";
import YAML from "yaml";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const BRIDGE_HOST = process.env.VIZ_BRIDGE_HOST || "0.0.0.0";
const BRIDGE_PORT = Number(
	process.env.VIZ_BRIDGE_PORT || process.env.VITE_VIZ_PORT || "6600",
);
const FRAME_HISTORY_LIMIT = Number(process.env.VIZ_FRAME_HISTORY || "0");
const CONTROL_URL = (process.env.TELEMETRY_CONTROL_URL || "").trim();
const CONFIG_PATH =
	process.env.SIX_CONFIG_PATH ||
	path.resolve(__dirname, "../../cmd/cfg/config.yml");

interface ProgramsConfig {
	programs?: Record<string, string>;
}

interface BridgeDiagnostics {
	bytesReceived: number;
	clients: number;
	droppedFrames: number;
	frameHistory: number;
	framesReceived: number;
	lastFrameAt: number | null;
}

const diagnostics: BridgeDiagnostics = {
	bytesReceived: 0,
	clients: 0,
	droppedFrames: 0,
	frameHistory: 0,
	framesReceived: 0,
	lastFrameAt: null,
};

const frameHistory: Buffer[] = [];

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

	for (const client of websocketServer.clients) {
		if (client === from) {
			continue;
		}
		if (client.readyState !== WebSocket.OPEN) {
			continue;
		}

		client.send(copy);
	}
}

async function loadProgramsFromConfig() {
	const source = await readFile(CONFIG_PATH, "utf8");
	const parsed = YAML.parse(source) as ProgramsConfig | null;

	return parsed?.programs ?? {};
}

async function loadPrograms() {
	if (!CONTROL_URL) {
		return loadProgramsFromConfig();
	}

	const response = await fetch(new URL("/programs", CONTROL_URL), {
		headers: { accept: "application/json" },
	});

	if (!response.ok) {
		throw new Error(`control programs request failed: ${response.status}`);
	}

	return (await response.json()) as Record<string, string>;
}

async function forwardPrompt(prompt: string) {
	if (!CONTROL_URL) {
		throw new Error("prompt control endpoint is not configured");
	}

	const response = await fetch(new URL("/prompt", CONTROL_URL), {
		body: JSON.stringify({ prompt }),
		headers: {
			"content-type": "application/json",
		},
		method: "POST",
	});

	const payload = (await response.json()) as unknown;

	if (!response.ok) {
		throw new Error(
			typeof payload === "object" && payload !== null && "error" in payload
				? String((payload as { error: unknown }).error)
				: `prompt request failed: ${response.status}`,
		);
	}

	return payload;
}

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

app.get("/api/diagnostics", (_req, res) => {
	res.json({
		controlUrl: CONTROL_URL || null,
		diagnostics,
		frameHistoryLimit: FRAME_HISTORY_LIMIT,
		ingest: "websocket",
		path: "/ws",
	});
});

app.get("/api/programs", async (_req, res) => {
	try {
		const programs = await loadPrograms();
		res.json(programs);
	} catch (error) {
		res.status(500).json({
			error: error instanceof Error ? error.message : String(error),
		});
	}
});

app.post("/api/prompt", async (req, res) => {
	const prompt =
		typeof req.body?.prompt === "string" ? req.body.prompt.trim() : "";

	if (!prompt) {
		res.status(400).json({ error: "prompt is required" });
		return;
	}

	try {
		const response = await forwardPrompt(prompt);
		res.json(response);
	} catch (error) {
		res.status(CONTROL_URL ? 502 : 501).json({
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

	if (FRAME_HISTORY_LIMIT > 0) {
		for (const frame of frameHistory) {
			client.send(frame);
		}
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
