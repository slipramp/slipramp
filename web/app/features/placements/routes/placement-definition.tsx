import { useOutletContext } from "react-router";

import { canonicalPlacementDefinition } from "~/features/placements/placement-definition";
import { YamlDefinitionSection } from "~/shared/yaml-definition-card";

import type { PlacementOutletContext } from "./placement";

export default function PlacementDefinition() {
  const { detail } = useOutletContext<PlacementOutletContext>();

  return <YamlDefinitionSection value={canonicalPlacementDefinition(detail.definition)} />;
}
