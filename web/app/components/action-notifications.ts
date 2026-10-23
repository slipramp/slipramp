import { notifications } from "@mantine/notifications";
import { useEffect, useRef } from "react";

export function useActionSuccessToast(
  actionData: unknown,
  title: string,
  message: string | undefined,
) {
  const shownActionData = useRef<unknown>(undefined);

  useEffect(() => {
    if (!actionData || shownActionData.current === actionData) {
      return;
    }

    shownActionData.current = actionData;
    if (message) {
      notifications.show({ color: "green", message, title });
    }
  }, [actionData, message, title]);
}
