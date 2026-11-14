import { Code, NativeSelect, NumberInput, SimpleGrid, Stack, Text, TextInput } from "@mantine/core";

import type { Parameter } from "~/api/generated";
import { parameterFieldName, sortedEntries } from "~/features/runs/action-run";

export function RunParameterFields({
  declarations,
  showTechnicalDetails = false,
  submittedValues,
}: Readonly<{
  declarations: Record<string, Parameter> | undefined;
  showTechnicalDetails?: boolean;
  submittedValues?: Record<string, string>;
}>) {
  const parameters = sortedEntries(declarations);

  if (parameters.length === 0) {
    return <Text c="dimmed">This action has no parameters.</Text>;
  }

  return (
    <SimpleGrid cols={{ base: 1, sm: 2 }}>
      {parameters.map(([name, parameter]) => (
        <ParameterControl
          key={name}
          name={name}
          parameter={parameter}
          showTechnicalDetails={showTechnicalDetails}
          submittedValue={submittedValues?.[name]}
        />
      ))}
    </SimpleGrid>
  );
}

function ParameterControl({
  name,
  parameter,
  showTechnicalDetails,
  submittedValue,
}: Readonly<{
  name: string;
  parameter: Parameter;
  showTechnicalDetails: boolean;
  submittedValue?: string;
}>) {
  const common = {
    description:
      parameter.description || showTechnicalDetails ? (
        <Stack gap={2}>
          {showTechnicalDetails && parameter.title ? (
            <Text c="dimmed" ff="monospace" size="xs">
              {name}
            </Text>
          ) : null}
          {parameter.description ? (
            <Text c="dimmed" size="xs">
              {parameter.description}
            </Text>
          ) : null}
          {showTechnicalDetails ? (
            <Text c="dimmed" size="xs">
              <ParameterDelivery parameter={parameter} />
            </Text>
          ) : null}
        </Stack>
      ) : undefined,
    label: parameter.title ?? name,
    name: parameterFieldName(name),
    required: parameter.required,
  };

  switch (parameter.type) {
    case "boolean":
      return (
        <NativeSelect
          {...common}
          data={selectOptions(parameter.required, ["true", "false"])}
          defaultValue={submittedValue ?? ""}
        />
      );
    case "enum":
      return (
        <NativeSelect
          {...common}
          data={selectOptions(parameter.required, parameter.values ?? [])}
          defaultValue={submittedValue ?? ""}
        />
      );
    case "number":
      return <NumberInput {...common} defaultValue={submittedValue ?? ""} />;
    case "string":
      return <TextInput {...common} defaultValue={submittedValue ?? ""} />;
  }
}

function ParameterDelivery({ parameter }: Readonly<{ parameter: Parameter }>) {
  if (parameter.environment) {
    return (
      <>
        Environment <Code>{parameter.environment}</Code>
      </>
    );
  }
  if (parameter.argument?.name) {
    return (
      <>
        Argument <Code>{parameter.argument.name}</Code>
      </>
    );
  }
  if (parameter.argument) return "Positional argument";
  return "Not mapped";
}

function selectOptions(required: boolean | undefined, values: string[]) {
  return [
    {
      disabled: required,
      label: required ? "Select a value" : "Not set",
      value: "",
    },
    ...values.map((value) => ({ label: value, value })),
  ];
}
