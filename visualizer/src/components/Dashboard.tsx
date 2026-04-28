import { useStore } from "@tanstack/react-store";
import { fieldStore } from "@/lib/field-store";
import { TopBar } from "./TopBar";
import { ValueDetail } from "./ValueDetail";
import { ValueGrid } from "./ValueGrid";

/*
Dashboard subscribes to the full field store in one shot rather than
fanning the read out across selectors. With React 19's external-store
hook, multiple inline selectors on the same store can drop a re-render
when only one slice changes during a high-rate telemetry burst — this
manifested as a selection that updated once and then froze. Reading the
whole state object is cheap (three fields) and removes that whole class
of bug.
*/
export function Dashboard() {
	const { values, selectedId, connectionError } = useStore(fieldStore);
	const selected = selectedId ? values.get(selectedId) ?? null : null;

	return (
		<div className="flex h-screen flex-col bg-[#05050f] text-white">
			<TopBar
				values={values}
				connectionError={connectionError}
				selectedId={selectedId}
			/>
			<div className="flex min-h-0 flex-1">
				<div className="flex-1 overflow-auto border-r border-white/10 p-3">
					<ValueGrid values={values} selectedId={selectedId} />
				</div>
				<div className="w-[480px] overflow-auto p-3">
					<ValueDetail stored={selected} values={values} />
				</div>
			</div>
		</div>
	);
}
