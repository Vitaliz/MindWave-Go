package mindwave

import (
    "time"
    "errors"
    "fmt"
    "sync"
    "github.com/tarm/serial"
)

var ErrHeadsetNotFound error = errors.New( "Headset no encontrado" )
var ErrNoHeadsetFound error = errors.New( "No hay headset disponibles" )
var ErrDisconnected error = errors.New( "Desconectado" )
var ErrRequestDenied error = errors.New( "Requerimiento denegado" )
var ErrChecksum error = errors.New( "Error de chechsum" )

type MindWave struct {
    port string
    ghid_high byte
    ghid_low byte
    connected bool
    conn *serial.Port
    mutex *sync.Mutex
    quit chan bool

    poorSignalQuality byte
    attentionESense byte
    meditationESense byte
    rawWave16Bit int16
}

func NewMindWave( port string, ghid_high byte, ghid_low byte ) *MindWave {
    var mw MindWave = MindWave {
        port: port,
        ghid_high: ghid_high,
        ghid_low: ghid_low,
        connected: false,
        mutex: &sync.Mutex{},
        quit: make( chan bool ),
    }
    return &mw
}

func ( mw *MindWave ) Connect() error {
    if( mw.connected ) {
        fmt.Println( "MindWave Connect(): ya se encuentra conectado a ", mw.port )
        return nil
    }

    fmt.Print( "MindWave Connect(): intentando conectar a ", mw.port, " => " )
    var err error
    var conn *serial.Port
    var c *serial.Config = &serial.Config { Name: mw.port, Baud: 115200, ReadTimeout: time.Millisecond * 500 }
    conn, err = serial.OpenPort( c )
    if( err != nil ) {
        fmt.Println( " ", err )
        return err
    }
    fmt.Println( "OK" )

    // desconecta y descarta lo recibido
    fmt.Print( "MindWave Connect(): Limpiando conexión previa => " )
    _, err = conn.Write( []byte { 0xc1 } )
    if( err != nil ) {
        conn.Close()
        fmt.Println( err )
        return err
    }
    time.Sleep( time.Millisecond * 1000 )
    conn.Flush()
    fmt.Println( "OK" )

    // conecta con/sin Global Headset Unique Identifier (ghid)
    if( mw.ghid_high != 0  || mw.ghid_low != 0) {
        fmt.Print( "MindWave Connect(): Enlazando headset => " )
        _, err = conn.Write( []byte { 0xc0, mw.ghid_high, mw.ghid_low } )
    } else {
        fmt.Print( "MindWave Connect(): Buscando headset => " )
        _, err = conn.Write( []byte { 0xc2 } )
    }
    if( err != nil ) {
        conn.Close()
        fmt.Println( err )
        return err
    }

    // esperamos la respuesta del dongle
    mw.conn = conn
    for {
        fmt.Print( "." )
        var payload []byte
        payload, err = mw.parsePacket()
        if( err != nil ) {
            if( err == ErrChecksum ) {
                continue
            }
            break
        }

        var cmd byte = payload[0]
        if( cmd == 0xd0 ) {                 // headset found and connected
            mw.ghid_high = payload[2]
            mw.ghid_low = payload[3]
            break
        }
        if( cmd == 0xd1 ) {                 // headset not found
            if( payload[1] == 0x00 ) {
                err = ErrNoHeadsetFound
            } else {
                err = ErrHeadsetNotFound
            }
            break
        }
        if( cmd == 0xd2 ) {                 // headset disconnected
            err = ErrDisconnected
            break
        }
        if( cmd == 0xd3 ) {                 // request denied
            err = ErrRequestDenied
            break
        }
        if( cmd == 0xd4 ) {                 // dongle in stand by mode
            if( payload[2] == 0x00 ) {
                break
            } else {                        // searching
                time.Sleep( time.Millisecond * 1000 )
            }
        } else {
            break
        }
    }

    if( err != nil ) {
        mw.conn.Close()
        mw.conn = nil
        fmt.Println( " ", err )
        return err
    }
    fmt.Println( " OK" )

    mw.connected = true
    go func( mw *MindWave ) {
        for {
            select {
                case <- mw.quit:
                    return
                default:
                    err := mw.parsePayload()
                    if( err != nil ) {
                        //fmt.Println( "\nMindWave: ", err )
                    }
                    // requerido para el scheduler
                    time.Sleep( time.Millisecond * 1 )
            }
        }
    }( mw )
    fmt.Println( "MindWave Connect(): Levantando tarea de lectura de datos" )
    time.Sleep( time.Millisecond * 1000 )
    return nil
}

func ( mw *MindWave ) Disconnect() {
    if( mw.connected ) {
        fmt.Print( "MindWave Disconnect(): Deteniendo Tareas => " )
        mw.quit <- true
        time.Sleep( time.Millisecond * 1000 )
        fmt.Println( "OK" )

        fmt.Print( "MindWave Disconnect(): Desconectando headset y cerrando puerta => " )
        mw.conn.Write( []byte { 0xc1 } )
        time.Sleep( time.Millisecond * 1000 )

        mw.conn.Flush()
        mw.conn.Close()
        mw.connected = false
        mw.conn = nil

        fmt.Println( "OK" )
    }
}

func ( mw *MindWave ) Connected() bool {
    return mw.connected
}

func ( mw *MindWave ) GlobalHeadsetID() string {
    return fmt.Sprintf( "%02X%02X", mw.ghid_high, mw.ghid_low )
}

func ( mw *MindWave ) PoorSignalQuality() byte {
    defer mw.mutex.Unlock()
    mw.mutex.Lock()
    return mw.poorSignalQuality
}

func ( mw *MindWave ) AttentionESense() byte {
    defer mw.mutex.Unlock()
    mw.mutex.Lock()
    return mw.attentionESense
}

func ( mw *MindWave ) MeditationESense() byte {
    defer mw.mutex.Unlock()
    mw.mutex.Lock()
    return mw.meditationESense
}

func ( mw *MindWave ) RawWave16Bit() int16 {
    defer mw.mutex.Unlock()
    mw.mutex.Lock()
    return mw.rawWave16Bit
}

func ( mw *MindWave ) parsePacket() ( []byte, error ) {
    var b []byte = make( []byte, 1 )
    var err error
    var inHeader bool = true
    var plength int = 0

    for inHeader {
        err = mw.read( b )
        if( err != nil ) {
            return nil, err
        }
        if( b[0] == 0xaa ) {
            err = mw.read( b )
            if( err != nil ) {
                return nil, err
            }
            if( b[0] == 0xaa ) {
                for {
                    err = mw.read( b )
                    if( err != nil ) {
                        return nil, err
                    }
                    plength = int( b[0] )
                    if( plength > 0xaa ) {
                        break
                    }
                    if( plength < 0xaa ) {
                        inHeader = false
                        break
                    }
                }
            }
        }
    }

    var payload []byte = make( []byte, plength )
    err = mw.read( payload )
    if( err!=nil ) {
        return nil, err
    }

    err = mw.read( b )
    if( err != nil ) {
        return nil, err
    }
    var checksum int
    checksum = int( b[0] )

    var sum int = 0
    for i:=0; i<plength; i++ {
        sum = sum + int( payload[i] )
    }
    sum = ( ^( sum & 0xff ) ) & 0xff
    if( checksum != sum ) {
        return nil, ErrChecksum
    } else {
        return payload, nil
    }
}

func ( mw *MindWave ) parsePayload() error {
    var payload []byte
    var err error

    payload, err = mw.parsePacket()
    if( err!= nil ) {
        return err
    }
    //fmt.Printf( "MindWave payload: [%x]\n", payload )

    if( payload[0] == 0xd2 ) {      // disconnected
        return ErrDisconnected
    }
    if( payload[0] == 0xd4 ) {      // alive message in stand by mode
        return nil
    }

    var length int
    var pos int = 0
    for pos<len(payload) {
        var exCodeLevel int = 0
        for payload[pos] == 0x55 {
            exCodeLevel = exCodeLevel + 1
            pos = pos + 1
        }
        var code int = int( payload[pos] )
        pos = pos + 1
        if( code & 0x80 == 0x80 ) {
            length = int( payload[pos] )
            pos = pos + 1
        } else {
            length = 1
        }

        var data []byte = make( []byte, length )
        for i:=0; i<length; i=i+1 {
            data[i] = payload[pos + i]
        }
        pos = pos + length

        if( exCodeLevel == 0 ) {
            switch code {
                case 0x02:  // poor signal quality (0 to 255)
                    mw.mutex.Lock()
                    mw.poorSignalQuality = data[0]
                    mw.mutex.Unlock()
                case 0x03:  // heart rate (0 to 255)
                case 0x04:  // attention eSense (0 to 100)
                    //fmt.Printf( "Payload: [%x], ExCodeLevel: %02x, Code: %02x [%x]\n", payload, exCodeLevel, code, data )
                    mw.mutex.Lock()
                    mw.attentionESense = data[0]
                    mw.mutex.Unlock()
                case 0x05:  // meditation eSense (0 to 100)
                    //fmt.Printf( "Payload: [%x], ExCodeLevel: %02x, Code: %02x [%x]\n", payload, exCodeLevel, code, data )
                    mw.mutex.Lock()
                    mw.meditationESense = data[0]
                    mw.mutex.Unlock()
                case 0x06:  // 8bit raw wave value (0 to 255)
                case 0x07:  // raw marker section start (0)
                case 0x80:  // raw wave value (-32768 to 32767) - big endian
                    mw.mutex.Lock()
                    var n int32 = ( int32( data[0] )<<8 ) + int32( data[1] )
                    if( ( n & 0x8000 ) != 0 ) {
                        n = n - 0xFFFF - 1
                    }
                    mw.rawWave16Bit = int16( n )
                    mw.mutex.Unlock()
                case 0x81:  // eeg power struct
                case 0x83:  // asic eeg power struct
                case 0x86:  // rrinterval (0 to 65535)
            }
        }
    }

    return nil
}

func ( mw *MindWave ) read( data[] byte ) error {
    var b []byte = make( []byte, 1 )
    var pos, n int
    var err error
    var nbytes int = len( data )

    for pos=0; pos < nbytes; {
        n, err = mw.conn.Read( b )
        if( err != nil ) {
            return err
        }
        if( n != 1 ){
            time.Sleep( time.Millisecond * 1 )
            continue
        }
        data[pos] = b[0]
        pos = pos + 1
    }
    return nil
}
