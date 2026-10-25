import { MascotPoseDetail } from "~/components/mascot-gallery";
import { findMascotPose } from "~/mascot-poses";
import { pageTitle } from "~/page-title";

import type { Route } from "./+types/mascot-pose";

export function meta({ params }: Route.MetaArgs) {
  const pose = findMascotPose(params.pose);

  return [{ title: pageTitle(pose?.label ?? "Mascot pose") }];
}

export default function MascotPoseRoute({ params }: Route.ComponentProps) {
  return <MascotPoseDetail pose={params.pose} />;
}
