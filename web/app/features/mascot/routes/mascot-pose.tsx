import { MascotPoseDetail } from "~/features/mascot/mascot-gallery";
import { findMascotPose } from "~/features/mascot/mascot-poses";
import { pageTitle } from "~/shared/page-title";

import type { Route } from "./+types/mascot-pose";

export function meta({ params }: Route.MetaArgs) {
  const pose = findMascotPose(params.pose);

  return [{ title: pageTitle(pose?.label ?? "Mascot pose") }];
}

export default function MascotPoseRoute({ params }: Route.ComponentProps) {
  return <MascotPoseDetail pose={params.pose} />;
}
