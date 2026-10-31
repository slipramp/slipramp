import { defineConfig } from "@hey-api/openapi-ts";

export default defineConfig({
  input: "ios/Slipramp/openapi.yaml",
  output: "web/app/api/generated",
  plugins: ["@hey-api/client-fetch", "@hey-api/typescript", "@hey-api/sdk"],
});
