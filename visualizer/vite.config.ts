
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import path from "path";
import { defineConfig, loadEnv } from "vite";

export default defineConfig(({ mode }) => {
	const env = loadEnv(mode, ".", "");
	return {
		plugins: [react(), tailwindcss()],
		define: {
			"process.env.GEMINI_API_KEY": JSON.stringify(env.GEMINI_API_KEY),
		},
		resolve: {
			alias: {
				"@": path.resolve(__dirname, "./src"),
			},
		},
		server: {
			port: 3000,
			host: "0.0.0.0",
			hmr: process.env.DISABLE_HMR !== "true",
			proxy: {
				"/ws": {
					target: "ws://127.0.0.1:6600",
					ws: true,
				},
				"/api": {
					target: "http://127.0.0.1:6600",
				},
			},
		},
		build: {
			target: "esnext",
			chunkSizeWarningLimit: 600,
			rollupOptions: {
				output: {
					manualChunks: {
						three: ["three"],
						react: ["react", "react-dom"],
					},
				},
			},
		},
	};
});
