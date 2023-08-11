let
  flakeCompat = import ./../..;
in [
  flakeCompat.packages.nardump
]
