import { FieldViewer } from "@/components/field-viewer/FieldViewer";
import { FieldProvider } from "@/context/field-context";

export default function App() {
	return (
		<FieldProvider>
			<FieldViewer />
		</FieldProvider>
	);
}
