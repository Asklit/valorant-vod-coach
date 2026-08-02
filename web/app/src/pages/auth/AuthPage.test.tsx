import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AuthPage } from "./AuthPage";

function authProps() {
  return {
    email: "",
    mode: "login" as const,
    name: "",
    password: "",
    setupToken: "",
    error: "",
    setupRequired: false,
    onEmailChange: vi.fn(),
    onModeChange: vi.fn(),
    onNameChange: vi.fn(),
    onPasswordChange: vi.fn(),
    onSetupTokenChange: vi.fn(),
    onSubmit: vi.fn()
  };
}

describe("AuthPage", () => {
  it("submits login credentials through controlled callbacks", () => {
    const props = authProps();
    render(<AuthPage {...props} />);

    fireEvent.change(screen.getByLabelText("Email"), { target: { value: "player@example.com" } });
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "safe-password" } });
    fireEvent.submit(screen.getByRole("button", { name: "Sign in" }).closest("form")!);

    expect(props.onEmailChange).toHaveBeenCalledWith("player@example.com");
    expect(props.onPasswordChange).toHaveBeenCalledWith("safe-password");
    expect(props.onSubmit).toHaveBeenCalledOnce();
  });

  it("shows the installation key only for first-user registration", () => {
    const props = authProps();
    render(<AuthPage {...props} mode="register" setupRequired />);

    expect(screen.getByLabelText("Name")).toBeInTheDocument();
    expect(screen.getByLabelText("Installation key")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create account" })).toBeInTheDocument();
  });
});
