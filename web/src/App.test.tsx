import { render, screen } from "@testing-library/react";
import { App } from "./App";

test("App を表示できる", () => {
  render(<App />);
  expect(screen.getByRole("main")).toHaveTextContent("libread");
});
