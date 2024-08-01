#!/usr/bin/env bash
hook_name="pre-commit"
hook_dir=".git/hooks"
osv_scanner=$( which osv-scanner )
if [ "$?" -eq 1 ]; then
echo "install osv-scanner into PATHs"
exit 0
fi
golangci_lint=$( which golangci-lint )
if [ "$?" -eq 1 ]; then
echo "install golangci-lint into PATHs"
exit 0
fi
gosec_bin=$( which gosec)
if [ "$?" -eq 1 ]; then
echo "install gosec into PATHs"
exit 0
fi
pre_script="#!/usr/bin/env bash
echo 'OSV Sanner:' && $osv_scanner . && echo 'golangci:' && $golangci_lint run . && echo 'Gosec:' && $gosec_bin -r
exit \$?"
echo "$pre_script" > "$hook_dir/$hook_name"
chmod u+x "$hook_dir/$hook_name"
