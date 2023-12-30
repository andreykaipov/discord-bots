{ pkgs, ... }:

{
  languages.typescript.enable = true;
  languages.javascript.enable = true;
  pre-commit.hooks.shellcheck.enable = true;

  packages = with pkgs; [
    _1password
    azure-cli
    jq
    entr
    git
    wrangler
    cloudflared
  ];

  enterShell = ''
    git --version
  '';
}
