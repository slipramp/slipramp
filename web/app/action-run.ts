import type {
  Action,
  Parameter,
  ParameterValue,
  ResourceRequirements,
  TargetDefinition,
} from "~/api/generated";

export function actionRunPath(targetId: string, actionName: string) {
  return `/targets/${targetId}/actions/${encodeURIComponent(actionName)}/run`;
}

export function otherTargetActions(
  actions: Record<string, Action> | undefined,
  currentAction: string,
) {
  return sortedEntries(actions)
    .filter(([name]) => name !== currentAction)
    .map(([name, action]) => ({ name, title: action.title }));
}

export function decodeActionParameters(
  formData: FormData,
  declarations: Record<string, Parameter> | undefined,
) {
  const parameters: Record<string, ParameterValue> = {};
  const values: Record<string, string> = {};

  for (const [name, declaration] of sortedEntries(declarations)) {
    const value = stringFormValue(formData, parameterFieldName(name));
    values[name] = value;

    if (value === "") {
      if (declaration.required) {
        return {
          error: `Parameter “${name}” is required.`,
          parameters,
          values,
        };
      }
      continue;
    }

    switch (declaration.type) {
      case "boolean":
        if (value !== "true" && value !== "false") {
          return {
            error: `Parameter “${name}” must be true or false.`,
            parameters,
            values,
          };
        }
        parameters[name] = value === "true";
        break;
      case "enum":
        if (!declaration.values?.includes(value)) {
          return {
            error: `Parameter “${name}” must be one of its allowed values.`,
            parameters,
            values,
          };
        }
        parameters[name] = value;
        break;
      case "number": {
        const number = Number(value);
        if (!Number.isFinite(number)) {
          return {
            error: `Parameter “${name}” must be a number.`,
            parameters,
            values,
          };
        }
        parameters[name] = number;
        break;
      }
      case "string":
        parameters[name] = value;
        break;
    }
  }

  return { error: undefined, parameters, values };
}

export function effectiveAction(target: TargetDefinition, action: Action) {
  return {
    credentials: {
      ...target.spec.credentials,
      ...action.credentials,
    },
    placement: action.placement ?? target.spec.placement,
    resources: {
      limits: {
        ...target.spec.resources?.limits,
        ...action.resources?.limits,
      },
      requests: {
        ...target.spec.resources?.requests,
        ...action.resources?.requests,
      },
    } satisfies ResourceRequirements,
    runtime: action.runtime ?? target.spec.runtime,
    timeout: action.timeout ?? target.spec.timeout,
  };
}

export function parameterDelivery(parameter: Parameter) {
  if (parameter.environment) return `Environment ${parameter.environment}`;
  if (parameter.argument?.name) return `Argument ${parameter.argument.name}`;
  if (parameter.argument) return "Positional argument";
  return "Not mapped";
}

export function parameterFieldName(name: string) {
  return `parameter:${name}`;
}

export function sortedEntries<T>(values: Record<string, T> | undefined) {
  return Object.entries(values ?? {}).toSorted(([left], [right]) => left.localeCompare(right));
}

function stringFormValue(formData: FormData, name: string) {
  const value = formData.get(name);
  return typeof value === "string" ? value : "";
}
