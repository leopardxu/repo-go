package git

// SetVerbose 设置是否显示详细输出
func (r *CommandRunner) SetVerbose(verbose bool) {
	r.Verbose = verbose
}

// SetQuiet 设置是否静默运行
func (r *CommandRunner) SetQuiet(quiet bool) {
	r.Quiet = quiet
}