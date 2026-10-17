import { index, route, type RouteConfig } from "@react-router/dev/routes";

export default [
  index("routes/index.tsx"),
  route("repositories", "routes/repositories.tsx"),
  route("repositories/:repositoryId", "routes/repository.tsx"),
] satisfies RouteConfig;
