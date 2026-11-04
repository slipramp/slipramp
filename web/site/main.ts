import "@fontsource-variable/manrope/wght.css";
import mascotUrl from "../app/features/mascot/assets/slipramp-mascot-hero.webp";
import "../shared/site-shell.css";
import "./site.css";

const mascot = document.querySelector<HTMLImageElement>("[data-mascot]");

if (mascot) mascot.src = mascotUrl;
