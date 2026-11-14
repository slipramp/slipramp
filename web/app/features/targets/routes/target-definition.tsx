import { useOutletContext } from "react-router";

import { canonicalTargetDefinition } from "~/features/targets/target-definition";
import { YamlDefinitionSection } from "~/shared/yaml-definition-card";

import type { TargetOutletContext } from "./target";

export default function TargetDefinition() {
  const { target } = useOutletContext<TargetOutletContext>();

  return <YamlDefinitionSection value={canonicalTargetDefinition(target.definition)} />;
}
