// @vitest-environment jsdom
// Configure React Act global flag to suppress environment warnings
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

import { describe, it, expect } from "vitest";
import { useEffect, useRef } from "react";
import { createRoot } from "react-dom/client";
import { act } from "react";

// Mock component showing the scroll restoration hook logic
function ScrollRestoreTestComponent({ selectedId }: { selectedId: string | null }) {
  const scrollPosRef = useRef(0);

  useEffect(() => {
    const mainPanel = document.querySelector('.main-panel');
    if (!mainPanel) return;

    const handleScroll = () => {
      if (!selectedId) {
        scrollPosRef.current = mainPanel.scrollTop;
      }
    };

    mainPanel.addEventListener('scroll', handleScroll);
    return () => mainPanel.removeEventListener('scroll', handleScroll);
  }, [selectedId]);

  useEffect(() => {
    if (!selectedId) {
      const mainPanel = document.querySelector('.main-panel');
      if (mainPanel) {
        mainPanel.scrollTop = scrollPosRef.current;
      }
    }
  }, [selectedId]);

  return (
    <main className="main-panel" style={{ height: '100px', overflowY: 'auto' }}>
      <div style={{ height: '500px' }}>Scroll Content</div>
    </main>
  );
}

describe("Scroll Restoration", () => {
  it("should preserve and restore scrollTop when returning to overview", async () => {
    const container = document.createElement("div");
    document.body.appendChild(container);

    const root = createRoot(container);

    // 1. Mount with selectedId = null (Overview page)
    await act(async () => {
      root.render(<ScrollRestoreTestComponent selectedId={null} />);
    });

    const mainPanel = container.querySelector(".main-panel") as HTMLElement;
    expect(mainPanel).toBeDefined();

    // 2. Set scroll position to 150px
    mainPanel.scrollTop = 150;
    
    // Trigger scroll event manually
    await act(async () => {
      mainPanel.dispatchEvent(new Event("scroll"));
    });

    // 3. Navigate to detail view (selectedId = "node-1")
    await act(async () => {
      root.render(<ScrollRestoreTestComponent selectedId="node-1" />);
    });

    // Reset scroll position to mock unmount/remount layout shift
    mainPanel.scrollTop = 0;

    // 4. Return to overview (selectedId = null)
    await act(async () => {
      root.render(<ScrollRestoreTestComponent selectedId={null} />);
    });

    // Scroll position should be restored back to 150px
    expect(mainPanel.scrollTop).toBe(150);

    // Cleanup
    await act(async () => {
      root.unmount();
    });
    document.body.removeChild(container);
  });
});
