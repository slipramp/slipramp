import { useOutletContext } from "react-router";

import { canonicalPlacementDefinition } from "~/features/placements/placement-definition";
import { YamlDefinitionCard } from "~/shared/yaml-definition-card";

import type { PlacementOutletContext } from "./placement";

export default function PlacementDefinition() {
  const { detail } = useOutletContext<PlacementOutletContext>();

  return (
    <YamlDefinitionCard
      description="Normalized loaded Placement configuration, including its Cluster constraint."
      value={canonicalPlacementDefinition(detail.definition)}
    />
  );
}
