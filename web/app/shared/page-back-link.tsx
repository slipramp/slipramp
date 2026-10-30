import { Anchor } from "@mantine/core";
import { IconArrowLeft } from "@tabler/icons-react";
import { Link } from "react-router";

export function PageBackLink({ label, to }: Readonly<{ label: string; to: string }>) {
  return (
    <Anchor className="page-back-link" component={Link} size="sm" to={to} underline="never">
      <IconArrowLeft aria-hidden="true" size={16} stroke={1.9} />
      <span>{label}</span>
    </Anchor>
  );
}
