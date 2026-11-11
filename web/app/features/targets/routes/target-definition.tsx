import { useOutletContext } from "react-router";

import { canonicalTargetDefinition } from "~/features/targets/target-definition";
import { YamlDefinitionCard } from "~/shared/yaml-definition-card";

import type { TargetOutletContext } from "./target";

export default function TargetDefinition() {
  const { target } = useOutletContext<TargetOutletContext>();

  return (
    <YamlDefinitionCard
      description="Normalized Target accepted from the catalog. Server-managed source and status fields are omitted."
      value={canonicalTargetDefinition(target.definition)}
    />
  );
}
