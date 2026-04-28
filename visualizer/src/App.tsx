import { Dashboard } from "@/components/Dashboard";
import { TelemetryConnection } from "@/context/field-context";

export default function App() {
	return (
		<TelemetryConnection>
			<Dashboard />
		</TelemetryConnection>
	);
}
