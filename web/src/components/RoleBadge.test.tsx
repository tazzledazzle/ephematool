import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { RoleBadge } from "./RoleBadge";

afterEach(cleanup);

describe("RoleBadge", () => {
  it.each([
    ["admin", "Admin"],
    ["creator", "Creator"],
    ["viewer", "Viewer"],
  ] as const)("renders %s as %s", (role, label) => {
    render(<RoleBadge role={role} />);
    expect(screen.getByText(label)).toBeTruthy();
  });
});
