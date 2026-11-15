import { reactRouter } from "@react-router/dev/vite";
import { defineConfig } from "vite";

const localHost = process.env.SLIPRAMP_LOCAL_HOST;

export default defineConfig({
  plugins: [reactRouter()],
  resolve: {
    tsconfigPaths: true,
  },
  server: {
    allowedHosts: localHost ? [localHost] : [],
    host: "0.0.0.0",
  },
});
