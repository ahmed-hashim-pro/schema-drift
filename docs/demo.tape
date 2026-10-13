# Terminal demo for the README. Regenerate with:  make demo
# Records the real CLI against examples/; nothing is staged.

Output docs/demo.gif

Set Shell "bash"
Set FontSize 14
Set Width 1300
Set Height 640
Set Padding 18
Set Theme "GitHub Dark"
Set TypingSpeed 30ms

Hide
Type "export PATH=/vhs:$PATH SCHEMA_DRIFT=/vhs/schema-drift PS1='$ ' && cd /vhs && clear"
Enter
Show

Type "schema-drift check examples/contracts/orders/v2.json examples/contracts/orders/v3.json"
Enter
Sleep 7s

Type "clear"
Enter
Sleep 300ms

Type "examples/gate-demo.sh    # a pull request against contracts on main, as CI sees it"
Enter
Sleep 9s

Type "clear"
Enter
Sleep 300ms

Type "./orders-consumer < examples/events/orders-v3.ndjson    # a v2 consumer meets v3 events"
Enter
Sleep 4s
