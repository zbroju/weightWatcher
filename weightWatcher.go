// Written 2015 by Marcin 'Zbroju' Zbroinski.
// Use of this source code is governed by a GNU General Public License
// that can be found in the LICENSE file.

//TODO: refactor to use gsqlitehandler
//TODO: refactor to use log for errors
//TODO: refactor to use time library
//TODO: design database to hold data for body fat
//TODO: design database to hold data for body dimensions
//TODO: code add/edit/remove/list data for body fat
//TODO: code add/edit/remove/list data for body dimension
//TODO: add report with bmi
//TODO: add report with body fat for different methods
//TODO: add report with gnuplot weight
//TODO: add report with gnuplot body fat
//TODO: add report with gnuplot body dimensions
package main

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/codegangsta/cli"
	_ "github.com/mattn/go-sqlite3"
	"github.com/zbroju/gprops"
	"os"
	"path"
	"strconv"
	"time"
)

// Config settings
const (
	CONF_DATAFILE      = "DATA_FILE"
	CONF_VERBOSE       = "VERBOSE"
	CONF_MOVINGAVERAGE = "MOVING_AVERAGE"
)

// Database properties
var DB_PROPERTIES = map[string]string{
	"applicationName": "weightWatcher",
	"databaseVersion": "1.0",
}

func main() {
	dataFile := ""
	verbose := false
	movingAverage := 7

	cli.CommandHelpTemplate = `NAME:
   {{.HelpName}} - {{.Usage}}
USAGE:
   {{.HelpName}}{{if .Subcommands}} [subcommand]{{end}}{{if .Flags}} [command options]{{end}} {{if .ArgsUsage}}{{.ArgsUsage}}{{else}}[arguments...]{{end}}{{if .Description}}
DESCRIPTION:
   {{.Description}}{{end}}{{if .Flags}}
OPTIONS:
   {{range .Flags}}{{.}}
   {{end}}{{ end }}{{if .Subcommands}}
SUBCOMMANDS:
    {{range .Subcommands}}{{join .Names ", "}}{{ "\t" }}{{.Usage}}
{{end}}{{ end }}
`

	// Loading properties from config file if exists
	configSettings := gprops.New()
	configFile, err := os.Open(path.Join(os.Getenv("HOME"), ".wwrc"))
	if err == nil {
		err = configSettings.Load(configFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "weightWatcher: syntax error in %s. Exit.\n", configFile.Name())
			return
		}
	}
	configFile.Close()
	if configSettings.Contains(CONF_DATAFILE) {
		dataFile = configSettings.Get(CONF_DATAFILE)
	}
	if configSettings.Contains(CONF_VERBOSE) {
		verbose, err = strconv.ParseBool(configSettings.Get(CONF_VERBOSE))
		if err != nil {
			verbose = false
		}
	}
	if configSettings.Contains(CONF_MOVINGAVERAGE) {
		movingAverage, err = strconv.Atoi(configSettings.Get(CONF_MOVINGAVERAGE))
		if err != nil {
			fmt.Fprintf(os.Stderr, "weightWatcher: syntax error in %s. Exit.\n", configFile.Name())
			return
		}
	}

	// Commandline arguments
	app := cli.NewApp()
	app.Name = "weightWatcher"
	app.Usage = "keeps track of your weight"
	app.Version = "0.1"
	app.Authors = []cli.Author{
		cli.Author{"Marcin 'Zbroju' Zbroinski", "marcin@zbroinski.net"},
	}

	// Flags definitions
	flagDate := cli.StringFlag{
		Name:  "date, d",
		Value: today(),
		Usage: "date of measurement (format: YYYY-MM-DD)",
	}
	flagVerbose := cli.BoolFlag{
		Name:        "verbose, b",
		Usage:       "show more output",
		Destination: &verbose,
	}
	flagWeight := cli.Float64Flag{
		Name: "weight, w",
		//		Value: 0,
		Usage: "measured weight",
	}
	flagFile := cli.StringFlag{
		Name:  "file, f",
		Value: dataFile,
		Usage: "data file",
	}
	flagId := cli.IntFlag{
		Name:  "id, i",
		Value: -1,
		Usage: "id of edited or removed object",
	}
	flagMovAv := cli.IntFlag{
		Name:  "average, a",
		Value: movingAverage,
		Usage: "measurement subset size for calculating moving average",
	}

	// Commands
	app.Commands = []cli.Command{
		{
			Name:    "init",
			Aliases: []string{"I"},
			Flags:   []cli.Flag{flagVerbose, flagFile},
			Usage:   "init a new data file specified by the user",
			Action:  cmdInit,
		},
		{
			Name:    "add",
			Aliases: []string{"A"},
			Flags:   []cli.Flag{flagVerbose, flagDate, flagWeight, flagFile},
			Usage:   "add a new measurement",
			Action:  cmdAddMeasurement,
		},
		{
			Name:    "edit",
			Aliases: []string{"E"},
			Flags:   []cli.Flag{flagVerbose, flagDate, flagWeight, flagFile, flagId},
			Usage:   "edit a measurement",
			Action:  cmdEditMeasurement,
		},
		{
			Name:    "remove",
			Aliases: []string{"R"},
			Flags:   []cli.Flag{flagVerbose, flagFile, flagId},
			Usage:   "remove a measurement",
			Action:  cmdRemoveMeasurement,
		},
		{
			Name:    "list",
			Aliases: []string{"L"},
			Flags:   []cli.Flag{flagVerbose, flagFile},
			Usage:   "lists all measurements",
			Action:  cmdListMeasurements,
		},
		{
			Name:    "show",
			Aliases: []string{"S"},
			Usage:   "show report",
			// Reports
			Subcommands: []cli.Command{
				{
					Name:    "history",
					Aliases: []string{"a"},
					Flags:   []cli.Flag{flagFile, flagMovAv},
					Usage:   "historical data with moving average (<x> periods)",
					Action:  reportHistory,
				},
			},
		},
	}
	app.Run(os.Args)
}

// cmdInit creates a new data file and add basic information about the file to properties table.
func cmdInit(c *cli.Context) {
	// Check the obligatory parameters and exit if missing
	if c.String("file") == "" {
		fmt.Fprint(os.Stderr, "weightWatcher: missing information about data file. Specify it with --file or -f flag.\n")
		return
	}

	// Check if file exist and if so - exit
	if _, err := os.Stat(c.String("file")); !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "weightWatcher: file %s already exists.\n", c.String("file"))
		return
	}

	// Open file
	db, err := sql.Open("sqlite3", c.String("file"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "weightWatcher: %s\n", err)
		return
	}
	defer db.Close()

	// Create tables
	sqlStmt := `
	BEGIN TRANSACTION;
	CREATE TABLE measurements (measurement_id INTEGER PRIMARY KEY, day DATE, measurement REAL);
		CREATE TABLE properties (key TEXT, value TEXT);
	COMMIT;
	`
	_, err = db.Exec(sqlStmt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "weightWatcher:  %s\n", err)
		return
	}

	// Insert properties values
	tx, err := db.Begin()
	if err != nil {
		fmt.Fprintf(os.Stderr, "weightWatcher: %s\n", err)
		return
	}
	stmt, err := tx.Prepare("INSERT INTO properties VALUES (?,?);")
	if err != nil {
		fmt.Fprint(os.Stderr, "weightWatcher: %s", err)
		return
	}
	defer stmt.Close()
	for key, value := range DB_PROPERTIES {
		_, err = stmt.Exec(key, value)
		if err != nil {
			fmt.Fprintf(os.Stderr, "weightWatcher: %s", err)
			tx.Rollback()
			return
		}
	}
	tx.Commit()

	// Show summary if verbose
	if c.Bool("verbose") == true {
		fmt.Fprintf(os.Stdout, "weightWatcher: created file %s.\n", c.String("file"))
	}
}

// cmdAddMeasurement adds measurement to data file
func cmdAddMeasurement(c *cli.Context) {

	// Check obligatory flags (file, date, measurement)
	if c.String("file") == "" {
		fmt.Fprintf(os.Stderr, "weightWatcher: missing file parameter. Specify it with --file or -f flag.\n")
		return
	}
	if c.String("date") == "" {
		fmt.Fprintf(os.Stderr, "weightWatcher: missing date parameter. Specify it with --date or -d flag.\n")
		return
	}
	if c.Float64("weight") == 0 {
		fmt.Fprintf(os.Stderr, "weightWatcher: missing weight parameter. Specify it with --weight or -w flag.\n")
		return
	}

	// Open data file
	db, err := getDataFile(c.String("file"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return
	}
	defer db.Close()

	// Add data to file
	sqlStmt := fmt.Sprintf("INSERT INTO measurements VALUES (NULL, '%s', %f);", c.String("date"), c.Float64("weight"))

	_, err = db.Exec(sqlStmt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "weightWatcher: %s\n", err)
		return
	}

	// Show summary if verbose
	if c.Bool("verbose") == true {
		fmt.Fprintf(os.Stdout, "weightWatcher: add measurement %3.2f to file %s with date %s.\n",
			c.Float64("weight"),
			c.String("file"),
			c.String("date"))
	}
}

// cmdEditMeasurement edit value or date for a measurement with given ID.
func cmdEditMeasurement(c *cli.Context) {
	// Check obligatory flags (id, file)
	if c.Int("id") < 0 {
		fmt.Fprintf(os.Stderr, "weightWatcher: missing ID parameter. Specify it with --id or -i flag.\n")
		return
	}
	if c.String("file") == "" {
		fmt.Fprintf(os.Stderr, "weightWatcher: missing file parameter. Specify it with --file or -f flag.\n")
		return
	}

	// Open data file
	db, err := getDataFile(c.String("file"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return
	}
	defer db.Close()

	// Check if measurement with given ID exists
	if !measurementExist(c.Int("id"), db) {
		fmt.Fprintf(os.Stderr, "weightWatcher: measurement with id=%d does not exist.\n", c.Int("id"))
		return
	}

	// Edit data
	sqlStmt := "BEGIN TRANSACTION;"
	if c.String("date") != "" {
		sqlStmt += fmt.Sprintf("UPDATE measurements SET day='%s' WHERE measurement_id=%d;", c.String("date"), c.Int("id"))
	}
	if c.Float64("weight") != 0 {
		sqlStmt += fmt.Sprintf("UPDATE measurements SET measurement=%f WHERE measurement_id=%d;", c.Float64("weight"), c.Int("id"))
	}
	sqlStmt += "COMMIT;"
	_, err = db.Exec(sqlStmt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "weightWatcher: %s\n", err)
		return
	}

	// Show summary if verbose
	if c.Bool("verbose") == true {
		fmt.Fprintf(os.Stdout, "weightWatcher: edited measurement %3.2f to file %s with date %s.\n",
			c.Float64("weight"),
			c.String("file"),
			c.String("date"))
	}
}

// cmdRemoveMeasurement removes measurement with given id.
func cmdRemoveMeasurement(c *cli.Context) {

	// Check obligatory flags (id, file)
	if c.Int("id") < 0 {
		fmt.Fprintf(os.Stderr, "weightWatcher: missing ID parameter. Specify it with --id or -i flag.\n")
		return
	}
	if c.String("file") == "" {
		fmt.Fprintf(os.Stderr, "weightWatcher: missing file parameter. Specify it with --file or -f flag.\n")
		return
	}

	// Open data file
	db, err := getDataFile(c.String("file"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return
	}
	defer db.Close()

	// Check if measurement with given ID exists
	if !measurementExist(c.Int("id"), db) {
		fmt.Fprintf(os.Stderr, "weightWatcher: measurement with id=%d does not exist.\n", c.Int("id"))
		return
	}

	// Remove measurements
	var sqlStmt string
	sqlStmt = fmt.Sprintf("DELETE FROM measurements WHERE measurement_id=%d;", c.Int("id"))
	_, err = db.Exec(sqlStmt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "weightWatcher: %s\n", err)
		return
	}

	// Show summary if verbose
	if c.Bool("verbose") == true {
		fmt.Fprintf(os.Stdout, "weightWatcher: deleted measurement with id=%d from file %s.\n",
			c.Int("id"),
			c.String("file"))
	}

}

// cmdListMeasurements lists all entries from database
func cmdListMeasurements(c *cli.Context) {

	// Check obligatory flags
	if c.String("file") == "" {
		fmt.Fprintf(os.Stderr, "weightWatcher: missing file parameter. Specify it with --file or -f flag.\n")
		return
	}

	// Open data file
	db, err := getDataFile(c.String("file"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return
	}
	defer db.Close()

	// Flush entries to standard out
	rows, err := db.Query("SELECT measurement_id, date(day), measurement FROM measurements ORDER BY day;")
	if err != nil {
		fmt.Fprintf(os.Stderr, "weightWatcher: error reading data file.\n")
		return
	}
	defer rows.Close()
	fmt.Printf("%+4s  %-10s  %11s\n", "ID", "DATE", "MEASUREMENT")
	for rows.Next() {
		var id int
		var day string
		var measurement float32
		rows.Scan(&id, &day, &measurement)
		fmt.Printf("%4d  %-10s  %11.2f\n", id, day, measurement)
	}
}

func reportHistory(c *cli.Context) {

	// Check obligatory flags
	if c.String("file") == "" {
		fmt.Fprintf(os.Stderr, "weightWatcher: missing file parameter. Specify it with --file or -f flag.\n")
		return
	}

	// Open data file
	db, err := getDataFile(c.String("file"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return
	}
	defer db.Close()

	// Calculate moving average and show results on standard output
	rows, err := db.Query("SELECT date(day), measurement FROM measurements ORDER BY day;")
	if err != nil {
		fmt.Fprintf(os.Stderr, "weightWatcher: error reading data file.\n")
		return
	}
	defer rows.Close()
	movingAverage := simpleMovingAverage(c.Int("average"))
	fmt.Printf("%-10s  %11s  %7s\n", "DATE", "MEASUREMENT", "AVERAGE")
	for rows.Next() {
		var day string
		var measurement float64
		rows.Scan(&day, &measurement)
		fmt.Printf("%-10s  %11.2f  %7.2f\n", day, measurement, movingAverage(measurement))
	}
}

// getDataFile checks if file exists and is a correct weightWatcher data file.
// If so, it returns pointer to the sql.DB, or otherwise nil and error.
func getDataFile(filePath string) (*sql.DB, error) {
	errorMessage := "weightWatcher: file " + filePath + " is not a correct weightWatcher data file."

	// Check if file exist and if not - exit
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, errors.New("weightWatcher: file " + filePath + " does not exist.")
	}

	// Open file
	db, err := sql.Open("sqlite3", filePath)
	if err != nil {
		return nil, errors.New(errorMessage)
	}

	// Check if the file is weightWatcher database
	rows, err := db.Query("SELECT key, value FROM properties;")
	if err != nil {
		return nil, errors.New(errorMessage)
	}
	if rows.Next() == false {
		return nil, errors.New(errorMessage)
	} else {
		for rows.Next() {
			var key, value string
			err = rows.Scan(&key, &value)
			if err != nil {
				return nil, errors.New(errorMessage)
			}
			if DB_PROPERTIES[key] != "" && DB_PROPERTIES[key] != value {
				return nil, errors.New(errorMessage)
			}
		}
	}
	rows.Close()

	return db, nil
}

// measurementExists returns true if a measurement with given id exists, or false otherwise.
func measurementExist(id int, db *sql.DB) bool {
	sqlStmt := fmt.Sprintf("SELECT measurement_id FROM measurements WHERE measurement_id=%d;", id)
	rows, err := db.Query(sqlStmt)
	defer rows.Close()
	if err == nil && rows.Next() {
		return true
	} else {
		return false
	}
}

// today returns string with actual date
func today() string {
	year, month, day := time.Now().Date()
	return dateString(year, int(month), day)
}

// dateString returns string with given year, month and day in the format: YYYY-MM-DD
func dateString(year, month, day int) string {
	yearString := strconv.Itoa(year)
	monthString := strconv.Itoa(month)
	dayString := strconv.Itoa(day)
	if len(dayString) < 2 {
		dayString = "0" + dayString
	}

	return yearString + "-" + monthString + "-" + dayString
}

// simpleMovingAverage returns function for moving average with period equals n.
func simpleMovingAverage(n int) func(float64) float64 {
	s := make([]float64, 0, n)
	i, sum, rn := 0, 0., 1/float64(n)
	return func(x float64) float64 {
		if len(s) < n {
			sum += x
			s = append(s, x)
			return sum / float64(len(s))
		}
		s[i] = x
		i++
		if i == n {
			i = 0
		}
		sum = 0
		for _, x = range s {
			sum += x
		}
		return sum * rn
	}
}
