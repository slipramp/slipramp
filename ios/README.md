# Slipramp for iOS

Use Slipramp on iPhone or iPad to start Actions, follow Runs, and review changes on your Slipramp Server.

- **Inbox** lists items that need attention. Open a pending Run or Promotion review to approve or reject it.
- **Targets** lets you find a Target, choose an Action, and set its parameters. If the Action needs an Artifact, start it from the Artifact in the Run that produced it.
- **Runs** shows live and saved logs, Artifacts, and Run status. You can request cancellation of an unfinished Run or rerun a finished one.
- **Promotions** shows Kargo Promotion history. Open a Promotion to inspect its Stage, Freight, and available Git or Argo CD diffs.

## Run the app

1. Open `ios/Slipramp.xcodeproj` in Xcode 16.3 or later.
2. Select the **Slipramp** scheme and an iOS 17 or later simulator or device. For a physical device, select your development team under **Signing & Capabilities** in the **Slipramp** target.
3. Click **Run**.

## Connect to your Server

Enter your Slipramp Server URL and tap **Check Server**. Use HTTPS, or confirm private HTTP if a trusted network such as a tailnet already encrypts the connection.

Once the compatibility check passes, tap **Sign in with SSO** if your Server offers it, or use a personal API key. The app saves your credential in the device Keychain.

Create a key under **Personal API keys** in the web app. Include `slipramp:read` for browsing and add scopes for the operations you need.

| Operation | Additional scope |
| --- | --- |
| Start, rerun, or cancel a Run | `slipramp:runs:write` |
| Approve or reject a Run review | `slipramp:runs:review` |
| Approve or reject a Promotion | `slipramp:promotions:write` |

Viewing sensitive Promotion diffs also requires an operator or admin role.

## Development

From the repository root, run `task ios:test` for the Swift package tests and `task ios:build` for a simulator build.

See the [development guide](../docs/DEVELOPMENT.md) for local Server setup and the [iOS design system](./DESIGN_SYSTEM.md) for UI components.
