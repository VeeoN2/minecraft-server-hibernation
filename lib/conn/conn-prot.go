package conn

import (
	"bytes"
	"encoding/json"
	"math/big"
	"net"
	"strings"

	"msh/lib/config"
	"msh/lib/errco"
	"msh/lib/model"
)

// buildMessage takes the request type and message to write to the client
func buildMessage(reqType int, message string) []byte {
	// mountHeader mounts the full header to a specified message
	var mountHeader = func(data []byte) []byte {
		//                  ┌--------------------full header--------------------┐
		// scheme:          [ sub-header1     | sub-header2 | sub-header3       | message   ]
		// bytes used:      [ 2               | 1           | 2                 | 0 - 16379 ]
		// value range:     [ 128 0 - 255 127 | 0           | 128 0 - 255 127	| --------- ]

		// addSubHeader mounts 1 sub-header to a specified message
		var addSubHeader = func(message []byte) []byte {
			//              ┌------sub-header1/3------┐
			// scheme:      [ firstByte | secondByte  | data ]
			// value range: [ 128 - 255 | 0 - 127     | ---- ]
			// it's a number composed of 2 digits in base-128 (firstByte is least significant byte)
			// sub-header represents the length of the following data

			firstByte := len(message)%128 + 128
			secondByte := float64(len(message) / 128)
			return append([]byte{byte(firstByte), byte(secondByte)}, message...)
		}

		// sub-header3 calculation
		data = addSubHeader(data)

		// sub-header2 calculation
		data = append([]byte{0}, data...)

		// sub-header1 calculation
		data = addSubHeader(data)

		return data
	}

	switch reqType {

	// send text to be shown in the loadscreen
	case errco.CLIENT_REQ_JOIN:
		messageStruct := &model.DataTxt{}
		messageStruct.Text = message

		dataTxtJSON, err := json.Marshal(messageStruct)
		if err != nil {
			// don't return error, just log it
			errco.LogMshErr(errco.NewErr(errco.ERROR_JSON_MARSHAL, errco.LVL_D, "buildMessage", err.Error()))
			return nil
		}

		return mountHeader(dataTxtJSON)

	// send server info
	case errco.CLIENT_REQ_INFO:

		// "&" [\x26] is converted to "§" [\xc2\xa7]
		// this step is not strictly necessary if in msh-config is used the character "§"
		message = strings.ReplaceAll(message, "&", "§")

		// replace "\\n" with "\n" in case the new line was set as msh parameter
		message = strings.ReplaceAll(message, "\\n", "\n")

		messageStruct := &model.DataInfo{}
		messageStruct.Description.Text = message
		messageStruct.Players.Max = 0
		messageStruct.Players.Online = 0
		messageStruct.Version.Name = config.ConfigRuntime.Server.Version
		messageStruct.Version.Protocol = config.ConfigRuntime.Server.Protocol
		messageStruct.Favicon = "data:image/png;base64," + config.ServerIcon

		dataInfJSON, err := json.Marshal(messageStruct)
		if err != nil {
			// don't return error, just log it
			errco.LogMshErr(errco.NewErr(errco.ERROR_JSON_MARSHAL, errco.LVL_D, "buildMessage", err.Error()))
			return nil
		}

		return mountHeader(dataInfJSON)

	default:
		return nil
	}
}

// getReqType returns the request type (INFO or JOIN) and playerName of the client
func getReqType(clientSocket net.Conn) ([]byte, int, string, *errco.Error) {
	dataReqFull := []byte{}
	playerName := "player unknown"

	data, errMsh := getClientPacket(clientSocket)
	if errMsh != nil {
		return nil, errco.ERROR_CLIENT_REQ, "", errMsh.AddTrace("getReqType")
	}

	dataReqFull = append(dataReqFull, data...)

	// generate flags
	listenPortByt := big.NewInt(int64(config.ListenPort)).Bytes() // calculates listen port in BigEndian bytes
	reqFlagInfo := append(listenPortByt, byte(1))                 // flag contained in INFO request packet -> [99 211 1]
	reqFlagJoin := append(listenPortByt, byte(2))                 // flag contained in JOIN request packet -> [99 211 2]

	// extract request type key byte
	reqTypeKeyByte := byte(0)
	if len(data) > int(data[0]) {
		reqTypeKeyByte = data[int(data[0])]
	}

	switch {
	case reqTypeKeyByte == byte(1) || bytes.Contains(data, reqFlagInfo):
		// client is requesting server info
		// example: [ 16 0 244 5 9 49 50 55 46 48 46 48 46 49 99 211 1 1 0 ]
		//  ______________ case 1 _______________      _____________ case 2 _____________
		// [ 16 ... x x x (listenPortBytes) 1 1 0] or [ 16 ... x x x (listenPortBytes) 1 ]
		// [              ^---reqFlagInfo---^    ]    [           ^---reqFlagInfo---^    ]
		// [    ^-------------16 bytes------^    ]    [    ^-------------16 bytes------^ ]

		return data, errco.CLIENT_REQ_INFO, playerName, nil

	case reqTypeKeyByte == byte(2) || bytes.Contains(data, reqFlagJoin):
		// client is trying to join the server
		// example: [ 16 0 244 5 9 49 50 55 46 48 46 48 46 49 99 211 2 ]
		//  _______________________ case 1 _______________________      ________________________ case 2 _________________________
		// [ 16 ... x x x (listenPortBytes) 2 x x x (player name) ] or [ 16 ... x x x (listenPortBytes) 2 ][ x x x (player name) ]
		// [              ^---reqFlagJoin---^                     ]    [              ^---reqFlagJoin---^ ][                     ]
		// [                                  ^--dataSplAft[1]--^ ]    [                 dataSplitAft[1]-╝][                     ]
		// [    ^-------------16 bytes------^                     ]    [    ^-------------16 bytes------^ ][                     ]

		dataSplAft := bytes.SplitAfter(data, reqFlagJoin)
		if len(dataSplAft[1]) > 0 {
			// case 1: player name is already contained in packet
			playerName = string(dataSplAft[1][3:])

		} else {
			// case 2: player name is contained in following packet
			data, errMsh = getClientPacket(clientSocket)
			if errMsh != nil {
				// this error is non-blocking: log the error and return "player unknown"
				errco.LogMshErr(errMsh.AddTrace("extractPlayerName"))
				playerName = "player unknown"
			}
			dataReqFull = append(dataReqFull, data...)
			playerName = string(data[3:])
		}

		return dataReqFull, errco.CLIENT_REQ_JOIN, playerName, nil

	default:
		return nil, errco.CLIENT_REQ_UNKN, "", errco.NewErr(errco.CLIENT_REQ_UNKN, errco.LVL_D, "getReqType", "client request unknown")
	}
}

// getPing responds to the ping request
func getPing(clientSocket net.Conn) *errco.Error {
	// read the first packet
	pingData, errMsh := getClientPacket(clientSocket)
	if errMsh != nil {
		return errMsh.AddTrace("getPing [1]")
	}

	switch {
	case bytes.Equal(pingData, []byte{1, 0}):
		// packet is [1 0]
		// read the second packet
		pingData, errMsh = getClientPacket(clientSocket)
		if errMsh != nil {
			return errMsh.AddTrace("getPing [2]")
		}

	case bytes.Equal(pingData[:2], []byte{1, 0}):
		// packet is [1 0 9 1 0 0 0 0 0 89 73 114]
		// remove first 2 bytes: [1 0 9 1 0 0 0 0 0 89 73 114] -> [9 1 0 0 0 0 0 89 73 114]
		pingData = pingData[2:]
	}

	// answer ping
	clientSocket.Write(pingData)

	errco.Logln(errco.LVL_E, "%smsh --> client%s:%v", errco.COLOR_PURPLE, errco.COLOR_RESET, pingData)

	return nil
}

// getClientPacket reads the client socket and returns only the bytes containing data
func getClientPacket(clientSocket net.Conn) ([]byte, *errco.Error) {
	buf := make([]byte, 1024)

	// read first packet
	dataLen, err := clientSocket.Read(buf)
	if err != nil {
		return nil, errco.NewErr(errco.ERROR_CLIENT_SOCKET_READ, errco.LVL_D, "getClientPacket", "error during clientSocket.Read()")
	}

	errco.Logln(errco.LVL_E, "%sclient --> msh%s:%v", errco.COLOR_PURPLE, errco.COLOR_RESET, buf[:dataLen])

	return buf[:dataLen], nil
}