import { Dashboard } from "@/components/Dashboard";
import { TelemetryConnection } from "@/context/telemetry-context";

export default function App() {
	return (
		<TelemetryConnection>
			<Dashboard />
		</TelemetryConnection>
	);
}
