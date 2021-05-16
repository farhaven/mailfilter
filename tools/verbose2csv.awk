/^{/{
	split($(NF-2), eta, ":")
	split($NF, score, ":")

	print NR ", " eta[2] " " score[2]
}