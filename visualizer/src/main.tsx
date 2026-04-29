import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App.tsx";
import "./index.css";

/*
Dev tooling JSON-stringifies component props or similar; JSON.stringify
throws on BigInt. Our wire decoder keeps Value frames as bigint[] so the
math against the 64-bit instruction words stays exact. Mutating BigInt’s
prototype in production risks surprising third-party serialization, so we
install the decimal-string toJSON helper only during development —
arithmetic always keeps bigint semantics.
*/
if (import.meta.env.DEV) {
	(BigInt.prototype as unknown as { toJSON(): string }).toJSON = function () {
		return this.toString();
	};
}

createRoot(document.getElementById("root") as HTMLElement).render(
	<StrictMode>
		<App />
	</StrictMode>,
);
