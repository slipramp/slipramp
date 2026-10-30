import type { SearchParamFilter } from "~/shared/list-controls";

export function promotionScopeFilters({
  application,
  integration,
  project,
}: Readonly<{
  application?: string;
  integration?: string;
  project?: string;
}>): SearchParamFilter[] {
  return [
    {
      active: Boolean(integration),
      kind: "text",
      label: "Integration",
      name: "integration",
      value: integration,
    },
    {
      active: Boolean(project),
      kind: "text",
      label: "Project",
      name: "project",
      value: project,
    },
    {
      active: Boolean(application),
      kind: "text",
      label: "Application",
      name: "application",
      value: application,
    },
  ];
}
