import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { EditMovieDialog } from "@/components/EditMovieDialog";

function renderDialog(initialLink = "https://www.imdb.com/title/tt0133093/") {
  const onSubmit = vi.fn();
  render(
    <EditMovieDialog
      isOpen
      onClose={vi.fn()}
      initialTitle="The Matrix"
      initialLink={initialLink}
      onSubmit={onSubmit}
    />,
  );
  return onSubmit;
}

function renderWatchedDialog(initialWatchedAt = "2026-07-21T20:00:37.123Z") {
  const onSubmit = vi.fn();
  render(
    <EditMovieDialog
      isOpen
      onClose={vi.fn()}
      initialTitle="The Matrix"
      initialLink="https://www.imdb.com/title/tt0133093/"
      initialWatchedAt={initialWatchedAt}
      allowWatchedAtEdit
      onSubmit={onSubmit}
    />,
  );
  return onSubmit;
}

describe("EditMovieDialog movie links", () => {
  it("keeps an unchanged TMDB link valid", () => {
    const link = "https://www.themoviedb.org/movie/603";
    const onSubmit = renderDialog(link);

    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    expect(onSubmit).toHaveBeenCalledWith({
      title: "The Matrix",
      link,
      watchedAt: undefined,
    });
  });

  it.each([
    "https://example.com/title/tt0133093/",
    "https://www.imdb.com.evil.test/title/tt0133093/",
  ])("explains and blocks an invalid link: %s", (link) => {
    const onSubmit = renderDialog();
    const input = screen.getByRole("textbox", { name: "Movie link" });

    fireEvent.change(input, { target: { value: link } });
    expect(screen.queryByRole("alert")).toBeNull();
    fireEvent.blur(input);

    expect(input.getAttribute("aria-invalid")).toBe("true");
    const error = screen.getByRole("alert");
    expect(error.textContent).toBe("Use an IMDb or TMDB movie URL.");
    expect(input.getAttribute("aria-describedby")).toBe(error.id);
    expect(
      screen
        .getByRole("button", { name: "Save changes" })
        .hasAttribute("disabled"),
    ).toBe(true);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("explains a required link after blur", () => {
    renderDialog();
    const input = screen.getByRole("textbox", { name: "Movie link" });

    fireEvent.change(input, { target: { value: "" } });
    fireEvent.blur(input);

    expect(input.getAttribute("aria-required")).toBe("true");
    expect(input.getAttribute("aria-invalid")).toBe("true");
    expect(screen.getByRole("alert").textContent).toBe(
      "Movie link is required.",
    );
    expect(
      screen
        .getByRole("button", { name: "Save changes" })
        .hasAttribute("disabled"),
    ).toBe(true);
  });

  it("clears the error and submits after a valid correction", () => {
    const onSubmit = renderDialog();
    const input = screen.getByRole("textbox", { name: "Movie link" });

    fireEvent.change(input, {
      target: { value: "https://example.com/movie/603" },
    });
    fireEvent.blur(input);
    expect(screen.queryByRole("alert")).not.toBeNull();

    const link = "https://www.themoviedb.org/movie/603-the-matrix";
    fireEvent.change(input, { target: { value: link } });

    expect(input.getAttribute("aria-invalid")).toBeNull();
    expect(input.getAttribute("aria-describedby")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    expect(onSubmit).toHaveBeenCalledWith({
      title: "The Matrix",
      link,
      watchedAt: undefined,
    });
  });
});

describe("EditMovieDialog watched date", () => {
  it("requires a watched date before saving", () => {
    const onSubmit = renderWatchedDialog();
    const input = screen.getByLabelText("Watched date and time");

    fireEvent.change(input, { target: { value: "" } });
    expect(screen.queryByRole("alert")).toBeNull();
    fireEvent.blur(input);

    expect(input.getAttribute("aria-required")).toBe("true");
    expect(input.getAttribute("aria-invalid")).toBe("true");
    const error = screen.getByRole("alert");
    expect(error.textContent).toBe("Watched date and time is required.");
    expect(input.getAttribute("aria-describedby")).toBe(error.id);
    expect(
      screen.getByRole("button", { name: "Save changes" }).hasAttribute("disabled"),
    ).toBe(true);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("omits an unchanged watched date to preserve its exact timestamp", () => {
    const onSubmit = renderWatchedDialog();

    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    expect(onSubmit).toHaveBeenCalledWith({
      title: "The Matrix",
      link: "https://www.imdb.com/title/tt0133093/",
      watchedAt: undefined,
    });
  });

  it("converts a changed local watched date to ISO", () => {
    const onSubmit = renderWatchedDialog();
    const input = screen.getByLabelText("Watched date and time");
    const changed = "2026-07-22T21:15";

    fireEvent.change(input, { target: { value: changed } });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    expect(onSubmit).toHaveBeenCalledWith({
      title: "The Matrix",
      link: "https://www.imdb.com/title/tt0133093/",
      watchedAt: new Date(changed).toISOString(),
    });
  });
});
