{ pkgs }:
{
  default = pkgs.mkShell {
    nativeBuildInputs = with pkgs; [
      actionlint
      gnumake
      go
      golangci-lint
      golangci-lint-langserver
      protobuf
      protoc-gen-go
      ratchet
      zizmor
    ];
  };
}
