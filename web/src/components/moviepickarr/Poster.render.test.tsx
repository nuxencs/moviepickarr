import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { Poster } from "@/components/moviepickarr/Poster";

afterEach(cleanup);

describe("Poster image sources", () => {
  it("keeps the existing w342 source and no responsive hints by default", () => {
    render(<Poster title="Possession" hue={42} posterPath="/possession.jpg" />);

    const image = screen.getByRole("img", { name: "Possession" });
    expect(image.getAttribute("src")).toBe(
      "https://image.tmdb.org/t/p/w342/possession.jpg",
    );
    expect(image.getAttribute("srcset")).toBeNull();
    expect(image.getAttribute("sizes")).toBeNull();
  });

  it("adds responsive candidates only when a caller supplies a size hint", () => {
    render(
      <Poster
        title="Possession"
        hue={42}
        posterPath="/possession.jpg"
        sizes="auto, 128px"
      />,
    );

    const image = screen.getByRole("img", { name: "Possession" });
    expect(image.getAttribute("src")).toBe(
      "https://image.tmdb.org/t/p/w342/possession.jpg",
    );
    expect(image.getAttribute("srcset")).toBe(
      "https://image.tmdb.org/t/p/w154/possession.jpg 154w, " +
        "https://image.tmdb.org/t/p/w185/possession.jpg 185w, " +
        "https://image.tmdb.org/t/p/w342/possession.jpg 342w, " +
        "https://image.tmdb.org/t/p/w500/possession.jpg 500w",
    );
    expect(image.getAttribute("sizes")).toBe("auto, 128px");
    expect(image.getAttribute("loading")).toBe("lazy");
  });
});
