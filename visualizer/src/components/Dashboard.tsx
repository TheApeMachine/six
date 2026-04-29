import { useStore } from "@tanstack/react-store";
import { fieldStore } from "@/lib/field-store";
import { TopBar } from "./TopBar";
import { ValueDetail } from "./ValueDetail";
import { ValueGrid } from "./ValueGrid";

/*
Dashboard subscribes to the full field store in one shot rather than
fanning the read out across selectors. Libraries that mirror React 18’s
useSyncExternalStore semantics can omit a render when finer-grained
selectors disagree during a telemetry burst — this manifested as a
selection that froze once the burst started. Pulling one snapshot via
TanStack Store’s useStore is cheap (three fields) and avoids that gap.
*/
export function Dashboard() {
	const { values, selectedId, connectionError } = useStore(fieldStore);
	const selected = selectedId ? (values.get(selectedId) ?? null) : null;

	return (
		<div className="flex h-screen flex-col bg-[#05050f] text-white">
			<TopBar
				values={values}
				connectionError={connectionError}
				selectedId={selectedId}
			/>
			<div className="flex min-h-0 flex-1 flex-col lg:flex-row">
				<div className="flex-1 overflow-auto lg:border-r border-white/10 p-3">
					<ValueGrid values={values} selectedId={selectedId} />
				</div>
				<div
					className={`min-h-0 w-full shrink-0 overflow-auto border-white/10 p-3 max-lg:border-t lg:w-[min(480px,100vw)] lg:max-w-[480px] lg:border-l lg:border-t-0 ${selected ? "" : "max-lg:hidden"}`}
				>
					<ValueDetail stored={selected} values={values} />
				</div>
			</div>
		</div>
	);
}
