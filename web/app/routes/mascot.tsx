import { MascotGallery } from "~/components/mascot-gallery";
import { pageTitle } from "~/page-title";

export function meta() {
  return [{ title: pageTitle("Mascot gallery") }];
}

export default function MascotGalleryRoute() {
  return <MascotGallery />;
}
