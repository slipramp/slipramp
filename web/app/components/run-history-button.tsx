import { Button } from "@mantine/core";
import { IconHistory } from "@tabler/icons-react";
import { Link } from "react-router";

export function RunHistoryButton({ to }: Readonly<{ to: string }>) {
  return (
    <Button
      component={Link}
      leftSection={<IconHistory size={16} />}
      size="compact-md"
      to={to}
      variant="light"
    >
      Run history
    </Button>
  );
}
