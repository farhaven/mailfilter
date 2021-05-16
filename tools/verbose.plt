set datafile separator ","

set y2range [-0.1:1.1]
set y2tics

set xzeroaxis

plot 'foo.csv' using 1:2 with lines title "Eta", \
	'foo.csv' using 1:3 with lines title "Score" axes x1y2

pause -1