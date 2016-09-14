package mindwave

import (
    "time"
    "errors"
    "fmt"
    "sync"
    "io"
    "github.com/tarm/serial"
)

var ErrHeadsetNotFound error = errors.New( "Headset no encontrado" )
var ErrNoHeadsetFound error = errors.New( "No hay Headset disponibles" )
var ErrDisconnected error = errors.New( "Desconectado" )
var ErrRequestDenied error = errors.New( "Requerimiento Denegado" )
var ErrChecksum error = errors.New( "Error de Checksum" )
var ErrZeroPlength error = errors.New( "Tamaño de payload es Cero" )

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
    blinkStrength byte
    rawWave16Bit int16
    delta uint32
    theta uint32
    lowAlpha uint32
    highAlpha uint32
    lowBeta uint32
    highBeta uint32
    lowGamma uint32
    midGamma uint32
}

type MindWaveData struct {
    PoorSignalQuality byte
    AttentionESense byte
    MeditationESense byte
    BlinkStrength byte
    RawWave16Bit int16
    Delta uint32
    Theta uint32
    LowAlpha uint32
    HighAlpha uint32
    LowBeta uint32
    HighBeta uint32
    LowGamma uint32
    MidGamma uint32
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
        fmt.Println( "MindWave Connect(): Ya se encuentra conectado a ", mw.port )
        return nil
    }

    fmt.Print( "MindWave Connect(): Intentando conectar a ", mw.port, " => " )
    var err error
    var conn *serial.Port
    var c *serial.Config = &serial.Config { Name: mw.port, Baud: 115200, ReadTimeout: time.Millisecond * 1 }
    conn, err = serial.OpenPort( c )
    if( err != nil ) {
        fmt.Println( " ", err )
        return err
    }
    fmt.Println( "OK" )

    // resetea conexión anterior
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
                time.Sleep( time.Millisecond * 1 )
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
    fmt.Println( "MindWave Connect(): Levantando tarea de lectura de datos" )
    go func( mw *MindWave ) {
        for {
            select {
                case <- mw.quit:
                    return
                default:
                    err = mw.parsePayload()
                    if( err != nil ) {
                        fmt.Println( "MindWave: ", err )
                    }
                    fmt.Println( mw.attentionESense )
                    // requerido para el scheduler
                    //time.Sleep( time.Millisecond * 1 )
            }
        }
    }( mw )
    time.Sleep( time.Millisecond * 1000 )
    return nil
}

func ( mw *MindWave ) Disconnect() {
    if( mw.connected ) {
        fmt.Print( "MindWave Disconnect(): Deteniendo Tarea => " )
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

func ( mw *MindWave ) MindWaveData() MindWaveData {
    defer mw.mutex.Unlock()
    mw.mutex.Lock()
    var mwd MindWaveData = MindWaveData {
        PoorSignalQuality: mw.poorSignalQuality,
        AttentionESense: mw.attentionESense,
        MeditationESense: mw.meditationESense,
        BlinkStrength: mw.blinkStrength,
        RawWave16Bit: mw.rawWave16Bit,
        Delta: mw.delta,
        Theta: mw.theta,
        LowAlpha: mw.lowAlpha,
        HighAlpha: mw.highAlpha,
        LowBeta: mw.lowBeta,
        HighBeta: mw.highBeta,
        LowGamma: mw.lowGamma,
        MidGamma: mw.midGamma,
    }
    return mwd
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

    if( plength <= 0 ) {
        return nil, ErrZeroPlength
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
    var checksum int = int( b[0] )
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

    if( payload[0] == 0xd2 ) {      // disconnected
        return ErrDisconnected
    }
    if( payload[0] == 0xd4 ) {      // alive message in stand by mode
        return nil
    }

    defer mw.mutex.Unlock()
    mw.mutex.Lock()

    var vlength int
    var pos int = 0
    for pos<len(payload) {
        var exCodeLevel int = 0
        for payload[pos] == 0x55 {
            exCodeLevel = exCodeLevel + 1
            pos = pos + 1
        }
        var code int = int( payload[pos] )
        pos = pos + 1
        if( code >= 0x80 ) {
            vlength = int( payload[pos] )
            pos = pos + 1
        } else {
            vlength = 1
        }

        var data []byte = make( []byte, vlength )
        for i:=0; i<vlength; i=i+1 {
            data[i] = payload[pos + i]
        }
        pos = pos + vlength

        if( exCodeLevel == 0 ) {
            switch code {
                case 0x02:  // poor signal quality (0 to 255) 200 => no skin contact
                    mw.poorSignalQuality = data[0]
                case 0x04:  // attention eSense (0 to 100) 40-60 => neutral, 0 => result is unreliable
                    mw.attentionESense = data[0]
                case 0x05:  // meditation eSense (0 to 100) 40-60 => neutral, 0 => result is unreliable
                    mw.meditationESense = data[0]
                case 0x16:  // blink strength (1 to 255)
                    mw.blinkStrength = data[0]
                case 0x80:  // raw wave value (-32768 to 32767) - big endian
                    var n int32 = int32( data[0] )<<8 + int32( data[1] )
                    if( n >= 32768 ) {
                        n = n - 65536
                    }
                    mw.rawWave16Bit = int16( n )
                case 0x83:  // asic eeg power struct (8, 3 bytes unsigned int big indian)
                    mw.delta = uint32( data[0] )<<16 + uint32( data[1] )<<8 + uint32( data[2] )
                    mw.theta = uint32( data[3] )<<16 + uint32( data[4] )<<8 + uint32( data[5] )
                    mw.lowAlpha = uint32( data[6] )<<16 + uint32( data[7] )<<8 + uint32( data[8] )
                    mw.highAlpha = uint32( data[9] )<<16 + uint32( data[10] )<<8 + uint32( data[11] )
                    mw.lowBeta = uint32( data[12] )<<16 + uint32( data[13] )<<8 + uint32( data[14] )
                    mw.highBeta = uint32( data[15] )<<16 + uint32( data[16] )<<8 + uint32( data[17] )
                    mw.lowGamma = uint32( data[18] )<<16 + uint32( data[19] )<<8 + uint32( data[20] )
                    mw.midGamma = uint32( data[21] )<<16 + uint32( data[22] )<<8 + uint32( data[23] )
                // case 0x01:  // code battery - battery low (0x00)
                // case 0x03:  // heart rate (0 to 255)
                // case 0x06:  // 8bit raw wave value (0 to 255)
                // case 0x07:  // raw marker section start (0)
                // case 0x81:  // eeg power struct (legacy float)
                // case 0x86:  // rrinterval (0 to 65535)
                default:
                    fmt.Printf( "ExCodeLevel: %02x, Code: %02x, Data: [%02x]\n", exCodeLevel, code, data )
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
    var tries int = 0

    for pos=0; pos < nbytes && tries < 500; {
        n, err = mw.conn.Read( b )
        if( err != nil && err != io.EOF ) {
            return err
        }
        if( n == 0 ){
            tries = tries + 1
            continue
        }
        data[pos] = b[0]
        pos = pos + 1
        tries = 0
    }
    if( pos < nbytes ) {
        return io.EOF
    }
    return nil
}
