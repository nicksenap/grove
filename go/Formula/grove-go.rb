class GroveGo < Formula
  desc "Git Worktree Workspace Orchestrator (Go)"
  homepage "https://github.com/nicksenap/grove"
  license "MIT"

  # Updated by CI on release
  url "https://github.com/nicksenap/grove/archive/refs/tags/v0.13.0.tar.gz"
  sha256 "PLACEHOLDER"
  version "0.13.0"

  depends_on "go" => :build

  conflicts_with "grove", because: "both install a `gw` binary"

  def install
    cd "go" do
      system "go", "build", *std_go_args(ldflags: "-s -w -X github.com/nicksenap/grove/cmd.Version=#{version}-go", output: bin/"gw")
    end
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gw --version")
  end
end
