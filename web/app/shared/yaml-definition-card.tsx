import { Button, Code, CopyButton, Paper, Stack } from "@mantine/core";
import { IconCheck, IconCopy } from "@tabler/icons-react";
import { useMemo } from "react";

import { SectionHeader } from "~/shared/page";
import { renderYamlDefinition } from "~/shared/yaml-definition";

export function YamlDefinitionCard({
  description,
  value,
}: Readonly<{ description: string; value: unknown }>) {
  const contents = useMemo(() => renderYamlDefinition(value), [value]);

  return (
    <Paper p="lg" withBorder>
      <Stack gap="md">
        <SectionHeader
          actions={
            <CopyButton timeout={2_000} value={contents}>
              {({ copied, copy }) => (
                <Button
                  color={copied ? "green" : undefined}
                  leftSection={copied ? <IconCheck size={16} /> : <IconCopy size={16} />}
                  onClick={copy}
                  size="compact-md"
                  variant="light"
                >
                  {copied ? "Copied" : "Copy YAML"}
                </Button>
              )}
            </CopyButton>
          }
          description={description}
          title="YAML definition"
        />
        <Code block style={{ maxHeight: 640, overflow: "auto", whiteSpace: "pre" }}>
          {contents}
        </Code>
      </Stack>
    </Paper>
  );
}
