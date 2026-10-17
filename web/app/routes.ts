import { index, route, type RouteConfig } from "@react-router/dev/routes";

export default [
  index("routes/index.tsx"),
  route("repositories", "routes/repositories.tsx"),
  route("repositories/:repositoryId", "routes/repository.tsx"),
  route("repositories/:repositoryId/targets/:targetName", "routes/target.tsx"),
  route("runs", "routes/runs.tsx"),
  route("runs/:runId", "routes/run.tsx"),
  route("api/v1/runs/:runId/logs", "routes/run-log.ts"),
] satisfies RouteConfig;
