import { MascotGallery } from "~/features/mascot/mascot-gallery";
import { pageTitle } from "~/shared/page-title";

export function meta() {
  return [{ title: pageTitle("Mascot gallery") }];
}

export default function MascotGalleryRoute() {
  return <MascotGallery />;
}
