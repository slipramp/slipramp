import { TextInput } from "@mantine/core";

export function PromotionScopeFilters({
  application,
  integration,
  project,
}: Readonly<{
  application?: string;
  integration?: string;
  project?: string;
}>) {
  return (
    <>
      <TextInput
        defaultValue={integration}
        label="Integration"
        name="integration"
        style={{ flex: "1 1 160px" }}
        type="search"
      />
      <TextInput
        defaultValue={project}
        label="Project"
        name="project"
        style={{ flex: "1 1 160px" }}
        type="search"
      />
      <TextInput
        defaultValue={application}
        label="Application"
        name="application"
        style={{ flex: "1 1 160px" }}
        type="search"
      />
    </>
  );
}
