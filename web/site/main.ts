import "@fontsource-variable/manrope/wght.css";
import mascotUrl from "../app/features/mascot/assets/slipramp-mascot.webp";
import "./site.css";

const mascot = document.querySelector<HTMLImageElement>("[data-mascot]");

if (mascot) mascot.src = mascotUrl;
