class SdoMcp < Formula
  desc "Model Context Protocol server for the Scrape.do web scraping API"
  homepage "https://scrape.do"
  license "Apache-2.0"

  livecheck do
    url :stable
    strategy :github_latest
  end

  on_macos do
    on_arm do
      url "https://github.com/scrape-do/sdo-mcp/releases/download/v0.2.0/sdo-mcp_0.2.0_darwin_arm64.tar.gz"
      sha256 "49470be4018df5e5713ae8cc6899d10f1d6e11235f2cc2b206cd1935c41da8a8"
    end
    on_intel do
      url "https://github.com/scrape-do/sdo-mcp/releases/download/v0.2.0/sdo-mcp_0.2.0_darwin_amd64.tar.gz"
      sha256 "5485c17cefefa5fee30d3715fcb4e1808c488dcffc40df8914cd6447cccc8c27"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/scrape-do/sdo-mcp/releases/download/v0.2.0/sdo-mcp_0.2.0_linux_arm64.tar.gz"
      sha256 "3fd4eebb0c601f90896b4d586227d2f337a182b47ab0848bc4d4557f3f8f2fac"
    end
    on_intel do
      url "https://github.com/scrape-do/sdo-mcp/releases/download/v0.2.0/sdo-mcp_0.2.0_linux_amd64.tar.gz"
      sha256 "6d082a72b302b874abe405978c269a2b71b59023b1d23a71f8af77d50cc2698a"
    end
  end

  def install
    bin.install "sdo-mcp"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/sdo-mcp version")
  end
end
