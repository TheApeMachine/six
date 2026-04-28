import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App.tsx";
import "./index.css";

/*
React 19's dev profiler logs component props by JSON-stringifying them,
and JSON.stringify throws on BigInt. Our wire decoder keeps Value frames
as bigint[] so the math against the 64-bit instruction words stays
exact. A toJSON shim returns the bigint as a decimal string for
serialization paths only — arithmetic and equality keep their bigint
semantics.
*/
(BigInt.prototype as unknown as { toJSON(): string }).toJSON = function () {
	return this.toString();
};

createRoot(document.getElementById("root") as HTMLElement).render(
	<StrictMode>
		<App />
	</StrictMode>,
);
