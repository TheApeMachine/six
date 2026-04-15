function envValue(name: string): string | undefined {
	const meta = import.meta as ImportMeta & { env?: Record<string, string> };
	return meta.env?.[name];
}

export function telemetryHost(): string {
	return envValue("VITE_VIZ_HOST") || "localhost";
}

export function telemetryPort(): string {
	return envValue("VITE_VIZ_PORT") || "6600";
}

export function telemetryHttpBase(): string {
	return `http://${telemetryHost()}:${telemetryPort()}`;
}

export function telemetryWebSocketURL(): string {
	return `ws://${telemetryHost()}:${telemetryPort()}/ws`;
}
