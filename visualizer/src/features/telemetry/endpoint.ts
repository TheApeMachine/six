function envValue(name: string): string | undefined {
	const meta = import.meta as ImportMeta & { env?: Record<string, string> };
	return meta.env?.[name];
}

function browserHost(): string | undefined {
	if (typeof window === "undefined") {
		return undefined;
	}

	return window.location.hostname || undefined;
}

function browserProtocol(): string {
	if (typeof window === "undefined") {
		return "http:";
	}

	return window.location.protocol;
}

export function telemetryHost(): string {
	return envValue("VITE_VIZ_HOST") || browserHost() || "localhost";
}

export function telemetryPort(): string {
	return envValue("VITE_VIZ_PORT") || (typeof window !== "undefined" ? window.location.port : "6600");
}

export function telemetryHttpBase(): string {
	const protocol = browserProtocol() === "https:" ? "https" : "http";
	return `${protocol}://${telemetryHost()}:${telemetryPort()}`;
}

export function telemetryWebSocketURL(): string {
	const protocol = browserProtocol() === "https:" ? "wss" : "ws";
	return `${protocol}://${telemetryHost()}:${telemetryPort()}/ws`;
}
