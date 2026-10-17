import { index, route, type RouteConfig } from "@react-router/dev/routes";

export default [
  index("routes/index.tsx"),
  route("repositories", "routes/repositories.tsx"),
  route("repositories/:repositoryId", "routes/repository.tsx"),
  route("repositories/:repositoryId/targets/:targetName", "routes/target.tsx"),
  route("runs/:runId", "routes/run.tsx"),
] satisfies RouteConfig;
